// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"FiReMQ/logging" // Локальный пакет с логированием в HTML файл
	"FiReMQ/pathsOS" // Локальный пакет с путями для разных платформ
)

// CheckResult содержит метаданные о последней доступной версии обновления
type CheckResult struct {
	Repo          string // "gitflic" | "github"
	RemoteVersion string // "дд.мм.гг"
	AssetName     string
	AssetURL      string
	ExpectedSHA   string // sha256 в hex
}

// updateChainManifest описывает цепочку обновлений для утилиты ServerUpdater
type updateChainManifest struct {
	CurrentVersion string            `json:"CurrentVersion"`
	Items          []updateChainItem `json:"Items"`
}

// updateChainItem описывает один релиз в цепочке
type updateChainItem struct {
	Version  string `json:"Version"`
	FileName string `json:"FileName"`
	Repo     string `json:"Repo"`
}

// Формат версии для time.Parse ("дд.мм.гг")
const versionLayout = "02.01.06"

// Sentinel-ошибки для сигнализации отсутствия обновлений/ассета
var ErrNoMatchingAsset = errors.New("подходящего обновления не найдено")
var ErrNoReleases = errors.New("обновлений нет")

// exeDir возвращает директорию, в которой находится исполняемый файл
func exeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// validVersion проверяет, соответствует ли строка ожидаемому формату версии (дд.мм.гг)
func validVersion(s string) bool {
	_, err := time.Parse(versionLayout, s)
	return err == nil
}

// isRemoteNewer сравнивает локальную и удаленную версии, возвращая true, если удаленная версия новее
func isRemoteNewer(local, remote string) (bool, error) {
	rt, err := time.Parse(versionLayout, remote)
	if err != nil {
		return false, fmt.Errorf("не удалось разобрать удалённую версию %q: %w", remote, err)
	}
	lt, err := time.Parse(versionLayout, local)
	if err != nil {
		// Считает, что обновление необходимо, если локальная версия имеет некорректный формат
		return true, nil
	}
	return rt.After(lt), nil
}

// ----- GitHub -----

// githubRelease представляет структуру данных релиза GitHub API
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// githubAsset представляет структуру данных ассета (файла) релиза GitHub API
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"` // Ожидает формат "sha256:<hex>"
}

// toAPIReleasesLatestURL преобразует пользовательский URL-адрес релиза GitHub в URL-адрес GitHub API releases/latest
func toAPIReleasesLatestURL(input string) (string, error) {
	u, err := url.Parse(input)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(u.Host)

	// Возвращает URL, если он уже является API-ссылкой
	if host == "api.github.com" && strings.HasPrefix(u.Path, "/repos/") && strings.HasSuffix(u.Path, "/releases/latest") {
		return u.String(), nil
	}
	// Конвертирует HTML-ссылку в API-ссылку
	if host == "github.com" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/") // owner/repo/releases/latest
		if len(parts) >= 4 && parts[2] == "releases" && parts[3] == "latest" {
			owner := parts[0]
			repo := parts[1]
			return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo), nil
		}
	}
	return "", fmt.Errorf("не удалось преобразовать URL %q к API releases/latest", input)
}

// fetchLatestGitHubRelease выполняет запрос к GitHub API и декодирует данные о последнем релизе
func fetchLatestGitHubRelease(apiURL string) (*githubRelease, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// Использует осмысленный User-Agent для соблюдения политики GitHub
	req.Header.Set("User-Agent", fmt.Sprintf("FiReMQ-Updater/1.0 (+%s)", pathsOS.Update_GitHubReleasesURL))
	// Добавляет токен авторизации, если он установлен в переменных окружения
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 20 * time.Second} // Устанавливает таймаут для предотвращения бесконечного ожидания
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("запрос к GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API вернул статус %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("ошибка декодирования JSON: %w", err)
	}
	return &rel, nil
}

// findAssetGitHub ищет ассет (файл) по заданному шаблону и извлекает версию из имени
func findAssetGitHub(rel *githubRelease) (githubAsset, string, error) {
	re := regexp.MustCompile(assetPattern)
	for _, a := range rel.Assets {
		if m := re.FindStringSubmatch(a.Name); m != nil {
			return a, m[1], nil // Группа 1 в шаблоне содержит версию
		}
	}
	return githubAsset{}, "", fmt.Errorf("%w: %q", ErrNoMatchingAsset, assetPattern)
}

// checkLatestFromGitHub проверяет наличие последнего релиза на GitHub
func checkLatestFromGitHub() (*CheckResult, error) {
	apiURL, err := toAPIReleasesLatestURL(pathsOS.Update_GitHubReleasesURL)
	if err != nil {
		return nil, fmt.Errorf("GitHub: некорректный URL релизов: %w", err)
	}
	rel, err := fetchLatestGitHubRelease(apiURL)
	if err != nil {
		return nil, err
	}
	asset, remoteVersion, err := findAssetGitHub(rel)
	if err != nil {
		return nil, err
	}
	// Проверяет, что контрольная сумма sha256 присутствует и имеет правильный префикс
	if asset.Digest == "" || !strings.HasPrefix(strings.ToLower(asset.Digest), "sha256:") {
		return nil, fmt.Errorf("GitHub: в ассете отсутствует корректный digest sha256 (получено: %q)", asset.Digest)
	}
	exp := strings.ToLower(strings.TrimPrefix(asset.Digest, "sha256:"))
	return &CheckResult{
		Repo:          "github",
		RemoteVersion: remoteVersion,
		AssetName:     asset.Name,
		AssetURL:      asset.BrowserDownloadURL,
		ExpectedSHA:   exp,
	}, nil
}

// ----- GitFlic -----

// gitflicReleases представляет корневую структуру ответа GitFlic API со списком релизов
type gitflicReleases struct {
	Embedded struct {
		ReleaseTagModelList []gitflicRelease `json:"releaseTagModelList"`
	} `json:"_embedded"`
}

// gitflicRelease представляет структуру данных релиза GitFlic
type gitflicRelease struct {
	TagName         string         `json:"tagName"`
	AttachmentFiles []gitflicAsset `json:"attachmentFiles"`
}

// gitflicAsset представляет структуру данных ассета (файла) релиза GitFlic
type gitflicAsset struct {
	Name       string `json:"name"`
	Link       string `json:"link"`
	HashSha256 string `json:"hashSha256"`
}

// toGitFlicAPIURL конвертирует HTML-ссылку GitFlic в соответствующий API-URL для получения релизов
func toGitFlicAPIURL(input string) (string, error) {
	u, err := url.Parse(input)
	if err != nil {
		return "", err
	}

	host := strings.ToLower(u.Host)
	if !strings.HasPrefix(host, "gitflic.ru") {
		return "", fmt.Errorf("некорректный хост для GitFlic: %s", host)
	}

	// Заменяет хост на API-домен
	u.Host = "api.gitflic.ru"
	return u.String(), nil
}

// fetchGitFlicReleases выполняет запрос к GitFlic API и декодирует список релизов
func fetchGitFlicReleases() (*gitflicReleases, error) {
	apiURL, err := toGitFlicAPIURL(pathsOS.Update_GitFlicReleasesURL)
	if err != nil {
		return nil, fmt.Errorf("GitFlic: некорректный URL релизов: %w", err)
	}
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("FiReMQ-Updater/1.0 (+%s)", pathsOS.Update_GitHubReleasesURL))

	// Добавляет токен авторизации GitFlic, если он предоставлен
	if pathsOS.Update_GitFlicToken != "" {
		req.Header.Set("Authorization", "token "+pathsOS.Update_GitFlicToken)
	}

	client := &http.Client{Timeout: 20 * time.Second} // Устанавливает таймаут для предотвращения бесконечного ожидания
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("запрос к GitFlic API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitFlic API вернул статус %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var rels gitflicReleases
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return nil, fmt.Errorf("ошибка декодирования JSON: %w", err)
	}
	return &rels, nil
}

// findLatestGitFlicRelease находит самый новый стабильный релиз с валидной версией
func findLatestGitFlicRelease(rels *gitflicReleases) (*gitflicRelease, error) {
	if len(rels.Embedded.ReleaseTagModelList) == 0 {
		return nil, ErrNoReleases
	}

	var latest *gitflicRelease
	var latestT time.Time

	for i := range rels.Embedded.ReleaseTagModelList {
		r := &rels.Embedded.ReleaseTagModelList[i]
		t, err := time.Parse(versionLayout, r.TagName)
		if err != nil {
			continue // Игнорирует релизы с некорректным форматом версии
		}
		// Находит релиз с наибольшим временем (самый новый)
		if latest == nil || t.After(latestT) {
			latest = r
			latestT = t
		}
	}
	if latest == nil {
		return nil, ErrNoReleases
	}
	return latest, nil
}

// findGitFlicAsset ищет ассет (файл) по заданному шаблону и извлекает версию из имени
func findGitFlicAsset(rel *gitflicRelease) (gitflicAsset, string, error) {
	re := regexp.MustCompile(assetPattern)
	for _, a := range rel.AttachmentFiles {
		if m := re.FindStringSubmatch(a.Name); m != nil {
			return a, m[1], nil // Группа 1 в шаблоне содержит версию
		}
	}
	return gitflicAsset{}, "", fmt.Errorf("%w: %q", ErrNoMatchingAsset, assetPattern)
}

// checkLatestFromGitFlic проверяет наличие последнего релиза на GitFlic
func checkLatestFromGitFlic() (*CheckResult, error) {
	rels, err := fetchGitFlicReleases()
	if err != nil {
		return nil, err
	}
	rel, err := findLatestGitFlicRelease(rels)
	if err != nil {
		return nil, err
	}
	asset, remoteVersion, err := findGitFlicAsset(rel)
	if err != nil {
		return nil, err
	}
	// Проверяет наличие контрольной суммы
	if strings.TrimSpace(asset.HashSha256) == "" {
		return nil, fmt.Errorf("GitFlic: отсутствует hashSha256 у ассета")
	}
	return &CheckResult{
		Repo:          "gitflic",
		RemoteVersion: remoteVersion,
		AssetName:     asset.Name,
		AssetURL:      asset.Link,
		ExpectedSHA:   strings.ToLower(asset.HashSha256),
	}, nil
}

// ----- Универсальные обёртки -----

// CheckLatest пытается получить информацию о последнем релизе, используя приоритетный репозиторий, с резервом
func CheckLatest() (*CheckResult, error) {
	var res *CheckResult
	var err error

	// Выполняет проверку на GitFlic, если он является первичным репозиторием
	if strings.EqualFold(pathsOS.Update_PrimaryRepo, "gitflic") {
		res, err = checkLatestFromGitFlic()
		if err == nil {
			return res, nil
		}
		logging.LogError("Обновление FiReMQ: Не удалось получить с GitFlic: %v — пробуем GitHub", err)
		// Возвращается к GitHub в случае ошибки
		return checkLatestFromGitHub()
	}

	// Выполняет проверку на GitHub, если он является первичным или GitFlic не указан
	res, err = checkLatestFromGitHub()
	if err == nil {
		return res, nil
	}
	logging.LogError("Обновление FiReMQ: Не удалось получить с GitHub: %v — пробуем GitFlic", err)
	// Возвращается к GitFlic в случае ошибки
	return checkLatestFromGitFlic()
}

// CheckAll возвращает список всех подходящих ассетов (по assetPattern) из приоритетного репозитория (с резервом на второй), используется для построения цепочки обновлений.
func CheckAll() ([]CheckResult, error) {
	var list []CheckResult
	var err error

	if strings.EqualFold(pathsOS.Update_PrimaryRepo, "gitflic") {
		list, err = checkAllFromGitFlic()
		if err == nil {
			return list, nil
		}
		logging.LogError("Обновление FiReMQ: Не удалось получить все релизы с GitFlic: %v — пробуем GitHub", err)
		return checkAllFromGitHub()
	}

	// GitHub — как первичный или не задан PrimaryRepo
	list, err = checkAllFromGitHub()
	if err == nil {
		return list, nil
	}
	logging.LogError("Обновление FiReMQ: Не удалось получить все релизы с GitHub: %v — пробуем GitFlic", err)
	return checkAllFromGitFlic()
}

// checkAllFromGitFlic возвращает все стабильные релизы с подходящими ассетами
func checkAllFromGitFlic() ([]CheckResult, error) {
	rels, err := fetchGitFlicReleases()
	if err != nil {
		return nil, err
	}
	if len(rels.Embedded.ReleaseTagModelList) == 0 {
		return nil, ErrNoReleases
	}

	var results []CheckResult
	for i := range rels.Embedded.ReleaseTagModelList {
		r := &rels.Embedded.ReleaseTagModelList[i]
		asset, remoteVersion, err := findGitFlicAsset(r)

		if err != nil {
			// Релиз без подходящего ассета — пропускается
			continue
		}

		if !validVersion(remoteVersion) {
			continue
		}

		if strings.TrimSpace(asset.HashSha256) == "" {
			continue
		}

		results = append(results, CheckResult{
			Repo:          "gitflic",
			RemoteVersion: remoteVersion,
			AssetName:     asset.Name,
			AssetURL:      asset.Link,
			ExpectedSHA:   strings.ToLower(asset.HashSha256),
		})
	}

	if len(results) == 0 {
		return nil, ErrNoMatchingAsset
	}
	return results, nil
}

// fetchGitHubReleasesList запрашивает полный список релизов GitHub (/releases)
func fetchGitHubReleasesList(apiURLLatest string) ([]githubRelease, error) {
	u, err := url.Parse(apiURLLatest)
	if err != nil {
		return nil, err
	}
	// /repos/<owner>/<repo>/releases/latest -> /repos/<owner>/<repo>/releases
	u.Path = strings.TrimSuffix(u.Path, "/latest")

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", fmt.Sprintf("FiReMQ-Updater/1.0 (+%s)", pathsOS.Update_GitHubReleasesURL))
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("запрос к GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API вернул статус %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var list []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("ошибка декодирования JSON: %w", err)
	}
	return list, nil
}

// checkAllFromGitHub возвращает все стабильные релизы с подходящими ассетами
func checkAllFromGitHub() ([]CheckResult, error) {
	apiURL, err := toAPIReleasesLatestURL(pathsOS.Update_GitHubReleasesURL)
	if err != nil {
		return nil, fmt.Errorf("GitHub: некорректный URL релизов: %w", err)
	}
	rels, err := fetchGitHubReleasesList(apiURL)
	if err != nil {
		return nil, err
	}
	if len(rels) == 0 {
		return nil, ErrNoReleases
	}

	var results []CheckResult
	for i := range rels {
		r := &rels[i]
		asset, remoteVersion, err := findAssetGitHub(r)

		if err != nil {
			continue
		}

		if !validVersion(remoteVersion) {
			continue
		}

		if strings.TrimSpace(asset.Digest) == "" ||
			!strings.HasPrefix(strings.ToLower(asset.Digest), "sha256:") {
			continue
		}
		exp := strings.ToLower(strings.TrimPrefix(asset.Digest, "sha256:"))

		results = append(results, CheckResult{
			Repo:          "github",
			RemoteVersion: remoteVersion,
			AssetName:     asset.Name,
			AssetURL:      asset.BrowserDownloadURL,
			ExpectedSHA:   exp,
		})
	}

	if len(results) == 0 {
		return nil, ErrNoMatchingAsset
	}
	return results, nil
}

// downloadWithChecksumStreaming скачивает файл по частям, вычисляет SHA256 и проверяет его соответствие ожидаемому значению
func downloadWithChecksumStreaming(urlStr, dest, expectedSHAHex string, extraHeaders map[string]string) error {
	const attempts = 2
	var lastErr error

	for i := 1; i <= attempts; i++ {
		_ = os.Remove(dest) // Удаляет файл, если он остался от предыдущей неудачной попытки

		req, err := http.NewRequest(http.MethodGet, urlStr, nil)
		if err != nil {
			lastErr = err
			continue
		}

		// Извлекает базовый URL для формирования User-Agent
		parsedURL, _ := url.Parse(urlStr)
		var baseURL string
		if parsedURL != nil {
			baseURL = parsedURL.Scheme + "://" + parsedURL.Host
		}

		req.Header.Set("User-Agent", fmt.Sprintf("FiReMQ-Updater/1.0 (+%s)", baseURL))
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Cache-Control", "no-cache")
		// Добавляет заголовки, специфичные для репозитория (например, токен GitFlic)
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}

		client := &http.Client{Timeout: 5 * time.Minute} // Устанавливает большой таймаут для скачивания больших файлов
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("скачивание: статус %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
			continue
		}

		out, err := os.Create(dest)
		if err != nil {
			_ = resp.Body.Close()
			lastErr = err
			continue
		}

		hasher := sha256.New()
		// Записывает данные одновременно в файл и в хешер
		mw := io.MultiWriter(out, hasher)
		buf := make([]byte, 256*1024)

		_, copyErr := io.CopyBuffer(mw, resp.Body, buf)
		closeOutErr := out.Close()
		closeRespErr := resp.Body.Close()

		// Проверяет все ошибки, связанные с передачей и закрытием потоков
		if copyErr != nil {
			_ = os.Remove(dest)
			lastErr = copyErr
			continue
		}
		if closeOutErr != nil {
			_ = os.Remove(dest)
			lastErr = closeOutErr
			continue
		}
		if closeRespErr != nil {
			_ = os.Remove(dest)
			lastErr = closeRespErr
			continue
		}

		// Проверяет вычисленную контрольную сумму
		sum := hex.EncodeToString(hasher.Sum(nil))
		if strings.EqualFold(sum, expectedSHAHex) {
			return nil // Успешное скачивание
		}

		// Обрабатывает ошибку контрольной суммы, которая приводит к повторной попытке
		_ = os.Remove(dest)
		lastErr = fmt.Errorf("контрольная сумма не совпала (ожидалось %s, получено %s) [попытка %d/%d]", expectedSHAHex, sum, i, attempts)
		logging.LogError("Обновление FiReMQ: Ошибка обновления: %v", lastErr)
	}

	return lastErr
}

// PrepareUpdate проверяет версию, при одной новой версии скачивает один архив (при нескольких - скачивает все и формирует "/tmp/update_chain.json" для последовательного обновления) во временной директории
func PrepareUpdate() (zipPath string, meta *CheckResult, err error) {
	if !validVersion(CurrentVersion) {
		return "", nil, fmt.Errorf("некорректный формат текущей версии %q (ожидается дд.мм.гг)", CurrentVersion)
	}

	exeBase, err := exeDir()
	if err != nil {
		return "", nil, fmt.Errorf("не удалось определить директорию FiReMQ: %w", err)
	}
	backupBase := strings.TrimSpace(pathsOS.Path_Backup)
	if backupBase == "" {
		backupBase = "Backup"
	}
	// Абсолютный путь к директории бэкапов
	if !filepath.IsAbs(backupBase) {
		backupBase = filepath.Join(exeBase, backupBase)
	}

	tmpDir := filepath.Join(backupBase, "tmp")
	_ = os.RemoveAll(tmpDir) // Удаляет старый tmp
	if err := pathsOS.EnsureDir(tmpDir); err != nil {
		return "", nil, fmt.Errorf("не удалось создать временную директорию %q: %w", tmpDir, err)
	}

	// Получает все релизы из репозитория
	all, err := CheckAll()
	if err != nil {
		return "", nil, err
	}

	// Сортирует по дате версии (дд.мм.гг) по возрастанию
	sort.SliceStable(all, func(i, j int) bool {
		ti, _ := time.Parse(versionLayout, all[i].RemoteVersion)
		tj, _ := time.Parse(versionLayout, all[j].RemoteVersion)
		return ti.Before(tj)
	})

	if len(all) == 0 {
		return "", nil, ErrNoReleases
	}

	// Отбирает только версии, строго новее текущей
	newer := make([]CheckResult, 0, len(all))
	for _, cr := range all {
		need, err := isRemoteNewer(CurrentVersion, cr.RemoteVersion)
		if err != nil {
			return "", nil, fmt.Errorf("ошибка сравнения версий: %w", err)
		}
		if need {
			newer = append(newer, cr)
		}
	}

	if len(newer) == 0 {
		latest := all[len(all)-1]
		return "", &latest, fmt.Errorf("обновление не требуется — локальная версия не старее (current=%s latest=%s)", CurrentVersion, latest.RemoteVersion)
	}

	// Если только одно обновление, скачивает и обновляет
	if len(newer) == 1 {
		m := newer[0] // Локальная копия
		assetPath := filepath.Join(tmpDir, m.AssetName)

		var headers map[string]string
		if strings.EqualFold(m.Repo, "gitflic") && pathsOS.Update_GitFlicToken != "" {
			headers = map[string]string{"Authorization": "token " + pathsOS.Update_GitFlicToken}
		}

		if err := downloadWithChecksumStreaming(m.AssetURL, assetPath, m.ExpectedSHA, headers); err != nil {
			return "", &m, fmt.Errorf("не удалось скачать ассет с корректной контрольной суммой: %w", err)
		}

		return assetPath, &m, nil
	}

	// Несколько обновлений — скачивает всю цепочку и формирует список релизов "update_chain.json" в tmp
	chain := updateChainManifest{
		CurrentVersion: CurrentVersion,
		Items:          make([]updateChainItem, 0, len(newer)),
	}

	for _, r := range newer {
		assetPath := filepath.Join(tmpDir, r.AssetName)

		var headers map[string]string
		if strings.EqualFold(r.Repo, "gitflic") && pathsOS.Update_GitFlicToken != "" {
			headers = map[string]string{"Authorization": "token " + pathsOS.Update_GitFlicToken}
		}

		if err := downloadWithChecksumStreaming(r.AssetURL, assetPath, r.ExpectedSHA, headers); err != nil {
			return "", nil, fmt.Errorf("не удалось скачать ассет %s с корректной контрольной суммой: %w", r.AssetName, err)
		}

		chain.Items = append(chain.Items, updateChainItem{
			Version:  r.RemoteVersion,
			FileName: r.AssetName,
			Repo:     r.Repo,
		})
	}

	chainPath := filepath.Join(tmpDir, "update_chain.json")
	data, err := json.MarshalIndent(chain, "", " ")
	if err != nil {
		return "", nil, fmt.Errorf("не удалось сериализовать update_chain.json: %w", err)
	}
	if err := os.WriteFile(chainPath, data, 0o644); err != nil {
		return "", nil, fmt.Errorf("не удалось записать update_chain.json: %w", err)
	}

	// В meta возвращает последнюю (целевую) версию цепочки
	last := newer[len(newer)-1]
	return chainPath, &last, nil
}
