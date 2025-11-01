// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package new_cert

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/flate"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"FiReMQ/pathsOS"

	"golang.org/x/net/idna"
)

const (
	defaultValidDays = 3650 // Длительность действия сертификата в днях (10 лет)
)

// certPaths Пути к ключевым PEM-файлам
type certPaths struct {
	ServerCA   string
	ServerCert string
	ServerKey  string

	ClientCA   string
	ClientCert string
	ClientKey  string
}

// sanValue Тип и значение SAN (Subject Alternative Name)
type sanValue struct {
	Kind  string // Тип значения ("IP" или "DNS")
	Value string
}

// EnsureMTLSCerts проверяет наличие комплекта mTLS сертификатов и при необходимости генерирует новый
func EnsureMTLSCerts(ctx context.Context, interactiveAllowed bool) error {
	paths := certPaths{
		ServerCA:   pathsOS.Path_Server_MQTT_CA,
		ServerCert: pathsOS.Path_Server_MQTT_Cert,
		ServerKey:  pathsOS.Path_Server_MQTT_Key,

		ClientCA:   pathsOS.Path_Client_MQTT_CA,
		ClientCert: pathsOS.Path_Client_MQTT_Cert,
		ClientKey:  pathsOS.Path_Client_MQTT_Key,
	}
	certsDir := filepath.Dir(paths.ServerCert)
	if err := pathsOS.EnsureDir(certsDir); err != nil {
		return fmt.Errorf("не удалось подготовить директорию сертов: %w", err)
	}

	ok, why := validateExisting(paths)
	if ok {
		return nil
	}

	// Краткая сводка обнаруженных проблем
	allMissing, missCnt, broken := summarizeProblems(why)
	switch {
	case allMissing:
		if interactiveAllowed {
			log.Println("Сертификаты отсутствуют, генерация нового комплекта в интерактивном режиме")
		} else {
			log.Println("Сертификаты отсутствуют")
		}
	case missCnt > 0 && len(broken) == 0:
		log.Printf("Неполный набор сертификатов (нет %d из 6) — генерирует новый комплект...", missCnt)
	case missCnt == 0 && len(broken) > 0:
		log.Printf("Обнаружены повреждённые файлы: %s — генерирует новый комплект...", compactList(broken, 3))
	default:
		log.Printf("Проблемы с сертификатами: нет: %d, повреждены: %s — генерирует новый комплект...", missCnt, compactList(broken, 3))
	}

	// Архивирует старые файлы перед удалением
	if err := archiveExisting(certsDir); err != nil {
		log.Printf("Не удалось заархивировать старые сертификаты: %v", err)
	}
	// Удаляет все старые сертификаты и артефакты, чтобы предотвратить дублирование при рестарте
	if n := deleteAllCertArtifacts(certsDir); n > 0 {
		log.Printf("Удалены старые сертификаты и артефакты: %d шт", n)
	}
	if err := ctxErr(ctx); err != nil {
		return err
	}

	// Поиск исполняемого файла OpenSSL
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		return fmt.Errorf("openssl не найден в PATH: %w", err)
	}

	// Определяет SAN интерактивно или из переменной окружения
	san, err := resolveSAN(ctx, allMissing && interactiveAllowed, interactiveAllowed)
	if err != nil {
		return fmt.Errorf("SAN не задан: %w", err)
	}
	if err := ctxErr(ctx); err != nil {
		return err
	}

	// Запускает процесс генерации всех сертификатов
	if err := generateAll(ctx, openssl, certsDir, paths, san); err != nil {
		if ctxErr := ctxErr(ctx); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("ошибка генерации сертификатов: %w", err)
	}

	// Повторная валидация после генерации
	ok, why = validateExisting(paths)
	if !ok {
		_, missCnt, broken = summarizeProblems(why)
		return fmt.Errorf("после генерации всё ещё проблемы (нет: %d, повреждены: %s)", missCnt, compactList(broken, 6))
	}

	log.Println("Новые сертификаты успешно созданы")
	return nil
}

// resolveSAN определяет значение SAN интерактивно или из переменной окружения
func resolveSAN(ctx context.Context, showHelp bool, allowInteractive bool) (sanValue, error) {
	// Интерактивный режим доступен для Windows и Linux с флагом -debug
	if allowInteractive {
		if showHelp {
			printInteractiveHelp()
		}
		fmt.Print("Введите SAN [Enter = localhost]: ")

		br := bufio.NewReader(os.Stdin)
		line, err := readLineCtx(ctx, br)
		if err != nil {
			return sanValue{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return sanValue{Kind: "DNS", Value: "localhost"}, nil
		}
		return parseSANString(line)
	}

	// В неинтерактивном режиме используется только переменная FIREMQ_SAN
	if runtime.GOOS == "linux" {
		if v := strings.TrimSpace(os.Getenv("FIREMQ_SAN")); v != "" {
			return parseSANString(v)
		}
		// Переменная не задана — печатает короткую подсказку и выходит
		log.Println("Не обнаружена переменная в юните (systemd):")
		log.Println("- Откройте 'sudo systemctl edit --full firemq.service'")
		log.Println("- Добавьте в секцию после [Service] строку: 'Environment=FIREMQ_SAN=77.77.77.77'")
		log.Println("- Перезапустите демона и FiReMQ: 'sudo systemctl daemon-reload && sudo systemctl restart firemq.service'")
		return sanValue{}, errors.New("переменная FIREMQ_SAN не установлена в unit (systemd)")
	}

	// Возврат к значению по умолчанию, если SAN не определён
	log.Println("Предупреждение: SAN не задан, используем localhost/127.0.0.1")
	return sanValue{Kind: "DNS", Value: "localhost"}, nil
}

// printInteractiveHelp выводит краткую справку по вводу SAN
func printInteractiveHelp() {
	fmt.Println()
	fmt.Println("СПРАВКА:")
	fmt.Println("Примеры указания SAN в интерактивном режиме:")
	fmt.Println("- Указание белого IP: 77.77.77.77")
	fmt.Println("- Указание домена: firemq.example.ru")
	fmt.Println()
	fmt.Println("Для НЕ интерактивного режима в Linux (systemd):")
	fmt.Println("- Используется переменная: 'Environment=FIREMQ_SAN=77.77.77.77' в юните")
	fmt.Println()
	fmt.Println("Установка или изменение переменной в unit:")
	fmt.Println("- Открыть редактор: 'sudo systemctl edit firemq.service'")
	fmt.Println("- Добавить или исправить строку в секции [Service]: 'Environment=FIREMQ_SAN=77.77.77.77'")
	fmt.Println("- Применить и перезапустить: 'sudo systemctl daemon-reload && sudo systemctl restart firemq.service'")
	fmt.Println()
}

// parseSANString парсит строку SAN (IP или DNS), поддерживая IDNA
func parseSANString(s string) (sanValue, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return sanValue{}, errors.New("пустое значение SAN")
	}

	// Запрещает двоеточия, чтобы избежать префиксов типа IP:/DNS: или указания портов
	if strings.Contains(s, ":") && !strings.Contains(s, "://") {
		return sanValue{}, errors.New("не используйте префиксы IP:/DNS: или порты — укажите только IP или домен")
	}

	// Проверяет, является ли строка IP-адресом
	if ip := net.ParseIP(s); ip != nil {
		return sanValue{Kind: "IP", Value: s}, nil
	}

	host := s // Переменная для обработки домена или IP

	// Удаляет суффикс пути, если присутствует
	if idx := strings.IndexByte(host, '/'); idx >= 0 {
		host = host[:idx]
	}

	// Удаляет указание порта, если присутствует
	if h, _, err := net.SplitHostPort(host); err == nil && h != "" {
		host = h
	}

	// Конвертирует в Punycode (IDNA)
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil || ascii == "" {
		return sanValue{}, fmt.Errorf("некорректный домен: %s", s)
	}

	if ascii != host {
		log.Printf("SAN (DNS) преобразован в Punycode: %s", ascii)
	}
	return sanValue{Kind: "DNS", Value: ascii}, nil
}

// validateExisting проверяет наличие, целостность и срок действия всех сертификатов
func validateExisting(p certPaths) (bool, []string) {
	var problems []string
	files := []string{p.ServerCA, p.ServerCert, p.ServerKey, p.ClientCA, p.ClientCert, p.ClientKey}
	for _, f := range files {
		if !fileExists(f) {
			problems = append(problems, fmt.Sprintf("нет файла: %s", f))
		}
	}
	if len(problems) > 0 {
		return false, problems
	}

	serverCA, err := readCert(p.ServerCA)
	if err != nil {
		problems = append(problems, fmt.Sprintf("битый server CA: %v", err))
	}
	clientCA, err := readCert(p.ClientCA)
	if err != nil {
		problems = append(problems, fmt.Sprintf("битый client CA: %v", err))
	}
	serverCert, err := readCert(p.ServerCert)
	if err != nil {
		problems = append(problems, fmt.Sprintf("битый server cert: %v", err))
	}
	clientCert, err := readCert(p.ClientCert)
	if err != nil {
		problems = append(problems, fmt.Sprintf("битый client cert: %v", err))
	}
	serverKey, err := readPrivateKey(p.ServerKey)
	if err != nil {
		problems = append(problems, fmt.Sprintf("битый server key: %v", err))
	}
	clientKey, err := readPrivateKey(p.ClientKey)
	if err != nil {
		problems = append(problems, fmt.Sprintf("битый client key: %v", err))
	}

	now := time.Now()
	for _, ce := range []struct {
		name string
		c    *x509.Certificate
	}{
		{"server cert", serverCert},
		{"client cert", clientCert},
		{"server CA", serverCA},
		{"client CA", clientCA},
	} {
		if ce.c == nil {
			continue
		}
		if now.Before(ce.c.NotBefore) || now.After(ce.c.NotAfter) {
			problems = append(problems, fmt.Sprintf("%s вне срока действия", ce.name))
		}
	}

	if serverCert != nil && serverKey != nil {
		if !pubKeysEqual(serverCert.PublicKey, publicFromPrivate(serverKey)) {
			problems = append(problems, "server-cert не соответствует server-key")
		}
	}
	if clientCert != nil && clientKey != nil {
		if !pubKeysEqual(clientCert.PublicKey, publicFromPrivate(clientKey)) {
			problems = append(problems, "client-cert не соответствует client-key")
		}
	}

	if serverCert != nil && serverCA != nil {
		if err := verifyChain(serverCert, serverCA, x509.ExtKeyUsageServerAuth); err != nil {
			problems = append(problems, "server-cert не верифицируется от server-CA")
		}
	}
	if clientCert != nil && clientCA != nil {
		if err := verifyChain(clientCert, clientCA, x509.ExtKeyUsageClientAuth); err != nil {
			problems = append(problems, "client-cert не верифицируется от client-CA")
		}
	}

	return len(problems) == 0, problems
}

// fileExists проверяет существует ли файл
func fileExists(f string) bool {
	_, err := os.Stat(f)
	return err == nil
}

// readCert парсит сертификат из PEM-файла
func readCert(path string) (*x509.Certificate, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var block *pem.Block
	for {
		block, b = pem.Decode(b)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		}
	}
	return nil, errors.New("CERTIFICATE не найден в PEM")
}

// readPrivateKey парсит приватный ключ из PEM-файла
func readPrivateKey(path string) (crypto.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var block *pem.Block
	for {
		block, b = pem.Decode(b)
		if block == nil {
			break
		}
		switch block.Type {
		case "PRIVATE KEY":
			return x509.ParsePKCS8PrivateKey(block.Bytes)
		case "RSA PRIVATE KEY":
			return x509.ParsePKCS1PrivateKey(block.Bytes)
		case "EC PRIVATE KEY":
			return x509.ParseECPrivateKey(block.Bytes)
		}
	}
	return nil, errors.New("приватный ключ не найден в PEM")
}

// publicFromPrivate извлекает публичный ключ из предоставленного приватного ключа
func publicFromPrivate(k crypto.PrivateKey) crypto.PublicKey {
	switch t := k.(type) {
	case *rsa.PrivateKey:
		return t.Public()
	case *ecdsa.PrivateKey:
		return t.Public()
	case ed25519.PrivateKey:
		return t.Public()
	default:
		return nil
	}
}

// pubKeysEqual сравнивает два публичных ключа
func pubKeysEqual(a, b crypto.PublicKey) bool {
	if a == nil || b == nil {
		return false
	}
	ab, err1 := x509.MarshalPKIXPublicKey(a)
	bb, err2 := x509.MarshalPKIXPublicKey(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}

// verifyChain проверяет, верифицируется ли сертификат-лист корневым CA для указанного типа использования
func verifyChain(leaf, ca *x509.Certificate, usage x509.ExtKeyUsage) error {
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	_, err := leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{usage},
	})
	return err
}

// archiveExisting архивирует существующие артефакты сертификатов в ZIP-файл с отметкой времени
func archiveExisting(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var toZip []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if hasAnySuffix(name, ".pem", ".srl", ".csr", ".cnf") {
			toZip = append(toZip, filepath.Join(dir, name))
		}
	}
	if len(toZip) == 0 {
		return nil
	}

	zipName := "old_bad_certs_" + time.Now().Format("02.01.06(15.04.05)") + ".zip"
	zipPath := filepath.Join(dir, zipName)

	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.BestCompression)
	})

	for _, p := range toZip {
		if err := addFileToZip(zw, p); err != nil {
			_ = zw.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	log.Printf("Архив старых сертификатов: %s", zipPath)
	return nil
}

// addFileToZip добавляет указанный файл в ZIP-архив
func addFileToZip(zw *zip.Writer, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	h, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	h.Name = filepath.Base(path)
	h.Method = zip.Deflate
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// hasAnySuffix проверяет наличие любого из указанных суффиксов без учета регистра
func hasAnySuffix(name string, suffixes ...string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(strings.ToLower(name), strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// generateAll использует OpenSSL для генерации всех необходимых CA, ключей и сертификатов
func generateAll(ctx context.Context, openssl, certsDir string, p certPaths, san sanValue) error {
	// Генерация корневого CA для сервера
	if err := runCmd(ctx, certsDir, openssl, "genpkey", "-algorithm", "RSA", "-out", "server-ca-key.pem", "-pkeyopt", "rsa_keygen_bits:2048"); err != nil {
		return fmt.Errorf("gen server-ca-key: %w", err)
	}
	if err := runCmd(ctx, certsDir, openssl, "req", "-x509", "-new", "-nodes",
		"-key", "server-ca-key.pem",
		"-sha256", "-days", fmt.Sprint(defaultValidDays),
		"-out", "server-cacert.pem",
		"-subj", "/CN=FiReMQ_Server_CA",
	); err != nil {
		return fmt.Errorf("req server-cacert: %w", err)
	}

	// Генерация корневого CA для клиента
	if err := runCmd(ctx, certsDir, openssl, "genpkey", "-algorithm", "RSA", "-out", "client-ca-key.pem", "-pkeyopt", "rsa_keygen_bits:2048"); err != nil {
		return fmt.Errorf("gen client-ca-key: %w", err)
	}
	if err := runCmd(ctx, certsDir, openssl, "req", "-x509", "-new", "-nodes",
		"-key", "client-ca-key.pem",
		"-sha256", "-days", fmt.Sprint(defaultValidDays),
		"-out", "client-cacert.pem",
		"-subj", "/CN=FiReMQ_Client_CA",
	); err != nil {
		return fmt.Errorf("req client-cacert: %w", err)
	}

	// Создает конфигурационный файл для сертификата сервера, включая SAN
	serverExt := buildServerExt(san)
	serverExtPath := filepath.Join(certsDir, "server-ext.cnf")
	if err := os.WriteFile(serverExtPath, []byte(serverExt), 0644); err != nil {
		return fmt.Errorf("write server-ext.cnf: %w", err)
	}

	// Генерация ключа, CSR и подпись сертификата сервера
	if err := runCmd(ctx, certsDir, openssl, "genpkey", "-algorithm", "RSA", "-out", "server-key.pem", "-pkeyopt", "rsa_keygen_bits:2048"); err != nil {
		return fmt.Errorf("gen server-key: %w", err)
	}
	if err := runCmd(ctx, certsDir, openssl, "req", "-new", "-key", "server-key.pem", "-out", "server.csr", "-config", "server-ext.cnf"); err != nil {
		return fmt.Errorf("req server.csr: %w", err)
	}
	if err := runCmd(ctx, certsDir, openssl, "x509", "-req", "-in", "server.csr",
		"-CA", "server-cacert.pem", "-CAkey", "server-ca-key.pem", "-CAcreateserial",
		"-out", "server-cert.pem", "-days", fmt.Sprint(defaultValidDays), "-sha256",
		"-extfile", "server-ext.cnf", "-extensions", "req_ext",
	); err != nil {
		return fmt.Errorf("x509 sign server-cert: %w", err)
	}

	// Создает конфигурационный файл для сертификата клиента
	clientExt := buildClientExt()
	clientExtPath := filepath.Join(certsDir, "client-ext.cnf")
	if err := os.WriteFile(clientExtPath, []byte(clientExt), 0644); err != nil {
		return fmt.Errorf("write client-ext.cnf: %w", err)
	}

	// Генерация ключа, CSR и подпись сертификата клиента
	if err := runCmd(ctx, certsDir, openssl, "genpkey", "-algorithm", "RSA", "-out", "client-key.pem", "-pkeyopt", "rsa_keygen_bits:2048"); err != nil {
		return fmt.Errorf("gen client-key: %w", err)
	}
	if err := runCmd(ctx, certsDir, openssl, "req", "-new", "-key", "client-key.pem", "-out", "client.csr", "-config", "client-ext.cnf"); err != nil {
		return fmt.Errorf("req client.csr: %w", err)
	}
	if err := runCmd(ctx, certsDir, openssl, "x509", "-req", "-in", "client.csr",
		"-CA", "client-cacert.pem", "-CAkey", "client-ca-key.pem", "-CAcreateserial",
		"-out", "client-cert.pem", "-days", fmt.Sprint(defaultValidDays), "-sha256",
		"-extfile", "client-ext.cnf", "-extensions", "req_ext",
	); err != nil {
		return fmt.Errorf("x509 sign client-cert: %w", err)
	}

	// Перемещает сгенерированные файлы на финальные пути
	if err := moveFile(filepath.Join(certsDir, "server-cacert.pem"), p.ServerCA); err != nil {
		return err
	}
	if err := moveFile(filepath.Join(certsDir, "server-cert.pem"), p.ServerCert); err != nil {
		return err
	}
	if err := moveFile(filepath.Join(certsDir, "server-key.pem"), p.ServerKey); err != nil {
		return err
	}
	if err := moveFile(filepath.Join(certsDir, "client-cacert.pem"), p.ClientCA); err != nil {
		return err
	}
	if err := moveFile(filepath.Join(certsDir, "client-cert.pem"), p.ClientCert); err != nil {
		return err
	}
	if err := moveFile(filepath.Join(certsDir, "client-key.pem"), p.ClientKey); err != nil {
		return err
	}

	// Удаляет временные файлы
	_ = os.Remove(filepath.Join(certsDir, "server.csr"))
	_ = os.Remove(filepath.Join(certsDir, "client.csr"))
	_ = os.Remove(filepath.Join(certsDir, "server-cacert.srl"))
	_ = os.Remove(filepath.Join(certsDir, "client-cacert.srl"))
	_ = os.Remove(clientExtPath)
	_ = os.Remove(serverExtPath)
	_ = os.Remove(filepath.Join(certsDir, "server-ca-key.pem"))
	_ = os.Remove(filepath.Join(certsDir, "client-ca-key.pem"))

	return nil
}

// runCmd запускает внешнюю команду и обеспечивает её отмену через контекст
func runCmd(ctx context.Context, dir, bin string, args ...string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	var errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("cmd: %s %s\nstdout: %s\nstderr: %s\nerr: %w",
			cmdup(bin), strings.Join(args, " "), out.String(), errb.String(), err)
	}
	return nil
}

// buildServerExt генерирует содержимое server-ext.cnf с учетом SAN
func buildServerExt(san sanValue) string {
	var alt []string
	switch san.Kind {
	case "IP":
		alt = append(alt, fmt.Sprintf("IP.1 = %s", san.Value))
		alt = append(alt, "DNS.1 = localhost")
		alt = append(alt, "IP.2 = 127.0.0.1")
	default: // DNS
		alt = append(alt, fmt.Sprintf("DNS.1 = %s", san.Value))
		alt = append(alt, "DNS.2 = localhost")
		alt = append(alt, "IP.1 = 127.0.0.1")
	}
	return "[ req ]\n" +
		"default_bits = 2048\n" +
		"prompt = no\n" +
		"encrypt_key = no\n" +
		"distinguished_name = dn\n" +
		"req_extensions = req_ext\n\n" +
		"[ dn ]\n" +
		fmt.Sprintf("CN = %s\n\n", san.Value) +
		"[ req_ext ]\n" +
		"subjectAltName = @alt_names\n" +
		"basicConstraints = CA:FALSE\n" +
		"keyUsage = digitalSignature, keyEncipherment\n" +
		"extendedKeyUsage = serverAuth\n\n" +
		"[ alt_names ]\n" +
		strings.Join(alt, "\n") + "\n"
}

// buildClientExt генерирует содержимое client-ext.cnf
func buildClientExt() string {
	return "[ req ]\n" +
		"default_bits = 2048\n" +
		"prompt = no\n" +
		"encrypt_key = no\n" +
		"distinguished_name = dn\n" +
		"req_extensions = req_ext\n\n" +
		"[ dn ]\n" +
		"CN = client\n\n" +
		"[ req_ext ]\n" +
		"subjectAltName = @alt_names\n" +
		"basicConstraints = CA:FALSE\n" +
		"keyUsage = digitalSignature, keyEncipherment\n" +
		"extendedKeyUsage = clientAuth\n\n" +
		"[ alt_names ]\n" +
		"DNS.1 = client\n"
}

// moveFile переносит файл, используя копирование и удаление, если os.Rename недоступен
func moveFile(src, dst string) error {
	if src == dst {
		return nil
	}
	if err := os.Rename(src, dst); err != nil {
		if err := copyFile(src, dst); err != nil {
			return err
		}
		_ = os.Remove(src)
	}
	return nil
}

// copyFile копирует содержимое файла
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// cmdup возвращает базовое имя исполняемого файла для логирования
func cmdup(bin string) string {
	if strings.Contains(bin, string(os.PathSeparator)) {
		return filepath.Base(bin)
	}
	return bin
}

// summarizeProblems составляет сводку проблем: количество отсутствующих и список поврежденных файлов
func summarizeProblems(why []string) (allMissing bool, missingCount int, broken []string) {
	total := 6
	missingCount = 0
	brokenSet := map[string]struct{}{}

	for _, w := range why {
		w = strings.ToLower(w)
		if strings.HasPrefix(w, "нет файла:") {
			missingCount++
			continue
		}
		switch {
		// ошибки чтения сертификатов/ключей
		case strings.Contains(w, "server cert"):
			brokenSet["server-cert.pem"] = struct{}{}
		case strings.Contains(w, "client cert"):
			brokenSet["client-cert.pem"] = struct{}{}
		case strings.Contains(w, "server key"):
			brokenSet["server-key.pem"] = struct{}{}
		case strings.Contains(w, "client key"):
			brokenSet["client-key.pem"] = struct{}{}
		case strings.Contains(w, "server ca"):
			brokenSet["server-cacert.pem"] = struct{}{}
		case strings.Contains(w, "client ca"):
			brokenSet["client-cacert.pem"] = struct{}{}
		// Сообщения с дефисами (несоответствие ключей/сертов)
		case strings.Contains(w, "server-cert"):
			brokenSet["server-cert.pem"] = struct{}{}
		case strings.Contains(w, "client-cert"):
			brokenSet["client-cert.pem"] = struct{}{}
		case strings.Contains(w, "server-key"):
			brokenSet["server-key.pem"] = struct{}{}
		case strings.Contains(w, "client-key"):
			brokenSet["client-key.pem"] = struct{}{}
		}
	}
	for k := range brokenSet {
		broken = append(broken, k)
	}
	allMissing = missingCount == total
	return
}

// compactList форматирует список строк, ограничивая вывод лимитом и добавляя "+N"
func compactList(list []string, limit int) string {
	if len(list) <= limit {
		return strings.Join(list, ", ")
	}
	rest := len(list) - limit
	return fmt.Sprintf("%s +%d", strings.Join(list[:limit], ", "), rest)
}

// readLineCtx читает строку из буферизированного ридера с возможностью отмены через контекст
func readLineCtx(ctx context.Context, r *bufio.Reader) (string, error) {
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := r.ReadString('\n')
		ch <- result{s: s, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-ch:
		return res.s, res.err
	}
}

// ctxErr возвращает ошибку контекста, если он был отменён или истёк
func ctxErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// deleteAllCertArtifacts удаляет все артефакты генерации сертификатов (.pem, .srl, .csr, .cnf)
func deleteAllCertArtifacts(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if hasAnySuffix(name, ".pem", ".srl", ".csr", ".cnf") {
			if err := os.Remove(filepath.Join(dir, name)); err == nil {
				count++
			}
		}
	}
	return count
}

// InitAndCheckMTLS инициализирует проверку и генерацию mTLS сертификатов, определяя режим работы (интерактив/сервис)
func InitAndCheckMTLS() {
	// Определяет, разрешен ли интерактивный режим ввода данных
	interactiveAllowed := false
	if runtime.GOOS == "windows" {
		interactiveAllowed = true // Интерактивный режим всегда разрешен в Windows
	} else if runtime.GOOS == "linux" && len(os.Args) > 1 && os.Args[1] == "-debug" {
		interactiveAllowed = true // Интерактивный режим в Linux разрешен только при использовании флага -debug
	}

	// Создает контекст, отменяемый при получении сигналов SIGINT или SIGTERM
	ctxCert, stopCert := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopCert()

	// Запускает проверку и создание mTLS сертификатов
	if err := EnsureMTLSCerts(ctxCert, interactiveAllowed); err != nil {
		if errors.Is(err, context.Canceled) {
			log.Println("Создание сертификатов отменено по сигналу")
			os.Exit(0)
		}
		log.Fatalf("Ошибка проверки/создания сертификатов для mTLS: %v", err)
	}
}
