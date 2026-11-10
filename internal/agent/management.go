package agent

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/plugin"
)

const (
	maxApplicationRequest = 12 << 20
	maxSettingsRequest    = 1 << 20
)

// Management 将 HTTP 管理接到宿主拥有的同一生命周期，批准不属于此接口。
type Management interface {
	ApplicationManager() *plugin.Manager
	ApplicationsChanged(id string)
	RefreshPage(id, page string) (PageResult, error)
}

// CandidatePreview is optional for hosts that expose pending candidates.
type CandidatePreview interface {
	PreviewInstallationPage(requestID, page, via string) (PageResult, error)
}

// PageResult 的元数据放在响应头中，保持原来的页面 JSON 兼容。
type PageResult struct {
	Page        any
	PackageHash string
	EvaluatedAt time.Time
	// Candidate previews explicitly report their remaining side effects.
	// Empty fields mean this is an ordinary installed-app refresh.
	Signing string
	Storage string
	Network string
}

func (s *Server) managed(w http.ResponseWriter) (Management, *plugin.Manager) {
	data, ok := s.data.(Management)
	if ok {
		if manager := data.ApplicationManager(); manager != nil {
			return data, manager
		}
	}
	writeCodeError(w, http.StatusServiceUnavailable, "MANAGEMENT_UNAVAILABLE", "this host has not enabled app management")
	return nil, nil
}

func (s *Server) catalog(w http.ResponseWriter, _ *http.Request) {
	managed := false
	if data, ok := s.data.(Management); ok {
		managed = data.ApplicationManager() != nil
	}
	writeJSON(w, http.StatusOK, Catalog(managed))
}

func (s *Server) example(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pkg, hash, err := plugin.OfficialPackage(id)
	if err != nil {
		writeCodeError(w, http.StatusNotFound, "EXAMPLE_NOT_FOUND", "no such official example")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "package": pkg, "packageHash": hash})
}

func decodeRequest(w http.ResponseWriter, r *http.Request, limit int64, out any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeCodeError(w, http.StatusUnsupportedMediaType, "JSON_REQUIRED", "the request must use application/json")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeCodeError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "the request body is too large")
		} else {
			writeCodeError(w, http.StatusBadRequest, "INVALID_JSON", "the request body could not be read")
		}
		return false
	}
	if err := plugin.DecodeJSON(data, out); err != nil {
		writeCodeError(w, http.StatusBadRequest, "INVALID_JSON", "the request contains invalid, unknown or duplicate fields, or trailing JSON")
		return false
	}
	return true
}

func (s *Server) validate(w http.ResponseWriter, r *http.Request) {
	_, manager := s.managed(w)
	if manager == nil {
		return
	}
	var req struct {
		Source plugin.Source `json:"source"`
	}
	if !decodeRequest(w, r, maxApplicationRequest, &req) {
		return
	}
	result, err := manager.Validate(req.Source)
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) propose(w http.ResponseWriter, r *http.Request) {
	data, manager := s.managed(w)
	if manager == nil {
		return
	}
	var req struct {
		Source       plugin.Source `json:"source"`
		ExpectedHash string        `json:"expectedHash,omitempty"`
	}
	if !decodeRequest(w, r, maxApplicationRequest, &req) {
		return
	}
	operation := "install"
	id := r.PathValue("id")
	if r.Method == http.MethodPut {
		operation = "update"
	} else if req.ExpectedHash != "" {
		writeCodeError(w, http.StatusBadRequest, "INVALID_BASE", "a new installation does not accept an update baseline")
		return
	}
	job, err := manager.Propose(req.Source, operation, id, req.ExpectedHash, "local-api")
	if err != nil {
		writeManagementError(w, err)
		return
	}
	data.ApplicationsChanged("")
	w.Header().Set("Location", "/v0/installations/"+job.RequestID)
	writeInstallation(w, http.StatusAccepted, job)
}

func writeInstallation(w http.ResponseWriter, status int, job plugin.Installation) {
	// 安装记录里的错误文案按码换成 API 的英文，不把插件层内部文本带出去。
	if key, ok := i18n.PluginErrorKey(job.Code); ok {
		job.Error = i18n.T(i18n.English, key)
	}
	writeJSON(w, status, struct {
		plugin.Installation
		StatusURL string `json:"statusUrl"`
	}{job, "/v0/installations/" + job.RequestID})
}

func (s *Server) installation(w http.ResponseWriter, r *http.Request) {
	data, manager := s.managed(w)
	if manager == nil {
		return
	}
	var job plugin.Installation
	var err error
	if r.Method == http.MethodDelete {
		job, err = manager.CancelFrom(r.PathValue("requestId"), "local-api")
	} else {
		job, err = manager.GetInstallation(r.PathValue("requestId"))
		if err == nil && job.Via == "acp" {
			writeCodeError(w, http.StatusNotFound, "INSTALLATION_NOT_FOUND", "安装任务不存在")
			return
		}
	}
	if err != nil {
		writeManagementError(w, err)
		return
	}
	if r.Method == http.MethodDelete {
		data.ApplicationsChanged("")
	}
	writeInstallation(w, http.StatusOK, job)
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	if _, manager := s.managed(w); manager == nil {
		return
	}
	data, ok := s.data.(CandidatePreview)
	if !ok {
		writeCodeError(w, http.StatusServiceUnavailable, "PREVIEW_UNAVAILABLE", "this host has not enabled candidate preview")
		return
	}
	result, err := data.PreviewInstallationPage(r.PathValue("requestId"), r.URL.Query().Get("page"), "local-api")
	if err != nil {
		writeManagementError(w, err)
		return
	}
	setRefreshMeta(w, result)
	writeJSON(w, http.StatusOK, result.Page)
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	data, manager := s.managed(w)
	if manager == nil {
		return
	}
	var req struct {
		Values map[string]string `json:"values"`
	}
	if !decodeRequest(w, r, maxSettingsRequest, &req) {
		return
	}
	if req.Values == nil {
		writeCodeError(w, http.StatusBadRequest, "VALUES_REQUIRED", "a values object is required")
		return
	}
	id := r.PathValue("id")
	if err := manager.APIMayMutate(id); err != nil {
		writeManagementError(w, err)
		return
	}
	settings, err := manager.SetSettings(id, req.Values)
	// 清空秘密是撤销配置，成功写入也不能当成可自动运行的配置保存。
	clearedSecret := false
	if err == nil {
		for _, field := range settings {
			value, supplied := req.Values[field.Key]
			if field.Secret && supplied && value == "" {
				clearedSecret = true
				break
			}
		}
	}
	// 旧宿主继续只接收变更通知。
	if observer, ok := data.(interface{ ApplicationSettingsChanged(string, bool, bool) }); ok {
		observer.ApplicationSettingsChanged(id, err == nil, clearedSecret)
	} else {
		data.ApplicationsChanged(id)
	}
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	data, manager := s.managed(w)
	if manager == nil {
		return
	}
	id := r.PathValue("id")
	if err := manager.APIMayMutate(id); err != nil {
		writeManagementError(w, err)
		return
	}
	if err := manager.Remove(id); err != nil {
		writeManagementError(w, err)
		return
	}
	data.ApplicationsChanged(id)
	w.WriteHeader(http.StatusNoContent)
}

func writeManagementError(w http.ResponseWriter, err error) {
	var known *plugin.Error
	if errors.As(err, &known) {
		status := known.Status
		if status < 400 || status > 599 {
			status = http.StatusInternalServerError
		}
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "2")
		}
		if key, ok := i18n.PluginErrorKey(known.Code); ok {
			writeCodeError(w, status, known.Code, i18n.T(i18n.English, key))
			return
		}
		writeCodeError(w, status, known.Code, known.Message)
		return
	}
	var partial *plugin.SettingsWriteError
	if errors.As(err, &partial) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "some settings could not be written; check their status", "code": "SETTINGS_WRITE_FAILED",
			"appliedKeys": partial.AppliedKeys, "failedKeys": partial.FailedKeys,
		})
		return
	}
	switch {
	case errors.Is(err, plugin.ErrNotInstalled), errors.Is(err, ErrNotFound):
		writeCodeError(w, http.StatusNotFound, "APP_NOT_FOUND", "no such app")
	case errors.Is(err, ErrDisabled), errors.Is(err, ErrNoPages):
		writeCodeError(w, http.StatusConflict, "APP_UNAVAILABLE", "the app is disabled or has no available page")
	default:
		// 运行时或上游异常可能含有应用读过的凭据，不能原样回传。
		writeCodeError(w, http.StatusBadGateway, "APP_EXECUTION_FAILED", "the app failed to execute; check its status in the shell")
	}
}

func writeCodeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}
