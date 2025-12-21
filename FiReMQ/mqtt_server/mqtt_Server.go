// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package mqtt_server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"FiReMQ/logging" // Локальный пакет с логированием в HTML файл
	"FiReMQ/pathsOS" // Локальный пакет с путями для разных платформ

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/config"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

// Инъекция реализаций из пакета "main"
var (
	SaveClientInfo          func(status, name, ip, localIP, clientID string) error
	HandleAnswerMessage     func(clientID string, payload []byte)
	HandleQUICAnswerMessage func(clientID, dateOfCreation, answer, quicExecution, attempts, description string)
)

// Server глобальная переменная для доступа к Mochi MQTT
var Server *mqtt.Server

// ClientMessage структура для парсинга JSON из сообщений
type ClientMessage struct {
	LocalIP string `json:"LocalIP"`
}

// versionHook Хук для проверки версии MQTT
type versionHook struct {
	mqtt.HookBase
}

// ID возвращает идентификатор хука
func (h *versionHook) ID() string {
	return "enforce-mqtt5"
}

// Provides сообщает, что хук обрабатывает событие OnConnect
func (h *versionHook) Provides(b byte) bool {
	return b == mqtt.OnConnect
}

// OnConnect проверяет, что клиент подключается с версией MQTT 5
func (h *versionHook) OnConnect(cl *mqtt.Client, pk packets.Packet) error {
	if pk.ProtocolVersion != 5 { // Разрешает подключаться только с MQTT версии 5.0, остальных отклоняет
		return fmt.Errorf("MQTT версии %d не разрешена, поддерживается только версия 5.0", pk.ProtocolVersion)
	}
	return nil
}

// Mqtt_serv инициализирует и запускает MQTT-сервер
func Mqtt_serv() {
	// Чтение конфига сервера из файла
	configBytes, err := os.ReadFile(pathsOS.Path_Config_MQTT)
	if err != nil {
		logging.LogError("MQTT Serv: Ошибка при чтении \"mqtt_config.json\": %v", err)
		os.Exit(1)
	}

	options, err := config.FromBytes(configBytes)
	// Парсинг конфига
	if err != nil {
		logging.LogError("MQTT Serv: Ошибка при парсинге \"mqtt_config.json\": %v", err)
		os.Exit(1)
	}

	// Явно отключает встроенный клиент Mochi-MQTT (используется AutoPaho)
	options.InlineClient = false

	// Создает сервер с настройками из конфигурационного файла
	Server = mqtt.New(options)

	// Добавляет хук для проверки версии MQTT
	Server.AddHook(&versionHook{}, nil)

	GetTopicData() // Настраивает обработку сообщений из топиков

	// Загружает сертификат и ключ сервера
	cert, err := tls.LoadX509KeyPair(pathsOS.Path_Server_MQTT_Cert, pathsOS.Path_Server_MQTT_Key)
	if err != nil {
		logging.LogError("MQTT Serv: Ошибка загрузки сертификатов: %v", err)
		os.Exit(1)
	}

	// Читает клиентский CA сертификат и добавляет его в пул доверенных сертификатов
	ClientCaCert, err := os.ReadFile(pathsOS.Path_Client_MQTT_CA)
	if err != nil {
		logging.LogError("MQTT Serv: Ошибка загрузки клиентского CA: %v", err)
		os.Exit(1)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(ClientCaCert) {
		logging.LogError("MQTT Serv: Не удалось добавить корневой сертификат в пул")
		os.Exit(1)
	}

	// Настраивает TLS с обязательной проверкой клиентских сертификатов (mTLS)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    certPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}

	// Создает TCP-слушатель с поддержкой TLS
	tcpListener := listeners.NewTCP(listeners.Config{
		ID:        "Server_FiReMQ",
		Address:   pathsOS.MQTT_Host + ":" + pathsOS.MQTT_Port,
		TLSConfig: tlsConfig,
	})
	err = Server.AddListener(tcpListener)

	if err != nil {
		logging.LogError("MQTT Serv: Ошибка добавления TCP слушателя MQTT: %v", err)
		os.Exit(1)
	}

	// Запускает сервер в отдельной горутине
	go func() {
		err := Server.Serve()
		if err != nil {
			logging.LogError("MQTT Serv: Критическая ошибка при запуске сервера: %v", err)
		}
	}()

}

// Stop корректно останавливает MQTT-сервер
func Stop() error {
	if Server != nil {
		return Server.Close()
	}
	return nil
}
