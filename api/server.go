package api

import (
	"encoding/json"
	"net/http"

	"riceguard/domain"
)

// Server exposes the JSON API and serves the built browser console.
type Server struct {
	svc       *Service
	staticDir string
}

// NewServer builds the HTTP server wiring a service and the built frontend
// output directory.
func NewServer(svc *Service, staticDir string) *Server {
	return &Server{svc: svc, staticDir: staticDir}
}

// Handler builds the routed HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/catalog", s.handleCatalog)
	mux.HandleFunc("GET /api/audit", s.handleAllAudit)
	mux.HandleFunc("GET /api/tasks", s.handleListTasks)
	mux.HandleFunc("POST /api/tasks", s.handleCreateTask)
	mux.HandleFunc("GET /api/tasks/{id}", s.handleGetTask)
	mux.HandleFunc("POST /api/tasks/{id}/sampling-confirmations", s.handleSampling)
	mux.HandleFunc("POST /api/tasks/{id}/split-blind-samples", s.handleSplit)
	mux.HandleFunc("POST /api/tasks/{id}/occupancies", s.handleOccupy)
	mux.HandleFunc("POST /api/tasks/{id}/germination-observations", s.handleGermination)
	mux.HandleFunc("POST /api/tasks/{id}/measurements/moisture-purity", s.handleMoisture)
	mux.HandleFunc("POST /api/tasks/{id}/pathogen-readings", s.handlePathogen)
	mux.HandleFunc("POST /api/tasks/{id}/reviews", s.handleReview)
	mux.HandleFunc("POST /api/tasks/{id}/finalize", s.handleFinalize)
	mux.HandleFunc("POST /api/tasks/{id}/rechamber", s.handleRechamber)
	mux.HandleFunc("POST /api/tasks/{id}/cancel", s.handleCancel)
	mux.HandleFunc("POST /api/tasks/{id}/rejudge", s.handleRejudge)
	mux.HandleFunc("GET /api/tasks/{id}/attempts", s.handleAttempts)
	mux.HandleFunc("GET /api/tasks/{id}/summary", s.handleSummary)
	mux.HandleFunc("GET /api/tasks/{id}/report", s.handleReport)

	mux.Handle("/", http.FileServer(http.Dir(s.staticDir)))
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.Catalog())
}

func (s *Server) handleAllAudit(w http.ResponseWriter, r *http.Request) {
	events, derr := s.svc.ListAllAudit()
	if derr != nil {
		writeError(w, derr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": events})
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, derr := s.svc.ListTasks()
	if derr != nil {
		writeError(w, derr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, domain.NewError(domain.CodeBadRequest, err.Error()))
		return
	}
	if req.OperationID == "" {
		writeError(w, domain.NewError(domain.CodeBadRequest, "operation_id is required"))
		return
	}
	resp, derr := s.svc.CreateTask(req)
	if derr != nil {
		writeError(w, derr)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	view, derr := s.svc.GetTask(id)
	if derr != nil {
		writeError(w, derr)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleSampling(w http.ResponseWriter, r *http.Request) {
	var req SamplingRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}
	resp, derr := s.svc.ConfirmSampling(r.PathValue("id"), req)
	writeResult(w, resp, derr, http.StatusOK)
}

func (s *Server) handleSplit(w http.ResponseWriter, r *http.Request) {
	var req SplitRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}
	resp, derr := s.svc.SplitBlindSamples(r.PathValue("id"), req)
	writeResult(w, resp, derr, http.StatusOK)
}

func (s *Server) handleOccupy(w http.ResponseWriter, r *http.Request) {
	var req OccupyRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}
	resp, derr := s.svc.Occupy(r.PathValue("id"), req)
	writeResult(w, resp, derr, http.StatusOK)
}

func (s *Server) handleGermination(w http.ResponseWriter, r *http.Request) {
	var req GerminationRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}
	resp, derr := s.svc.RecordGermination(r.PathValue("id"), req)
	writeResult(w, resp, derr, http.StatusOK)
}

func (s *Server) handleMoisture(w http.ResponseWriter, r *http.Request) {
	var req MoistureRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}
	resp, derr := s.svc.RecordMoisture(r.PathValue("id"), req)
	writeResult(w, resp, derr, http.StatusOK)
}

func (s *Server) handlePathogen(w http.ResponseWriter, r *http.Request) {
	var req PathogenRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}
	resp, derr := s.svc.RecordPathogen(r.PathValue("id"), req)
	writeResult(w, resp, derr, http.StatusOK)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	var req ReviewRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}
	resp, derr := s.svc.Review(r.PathValue("id"), req)
	writeResult(w, resp, derr, http.StatusOK)
}

func (s *Server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	var req FinalizeRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}
	resp, derr := s.svc.Finalize(r.PathValue("id"), req)
	writeResult(w, resp, derr, http.StatusOK)
}

func (s *Server) handleRechamber(w http.ResponseWriter, r *http.Request) {
	var req RechamberRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}
	resp, derr := s.svc.Rechamber(r.PathValue("id"), req)
	writeResult(w, resp, derr, http.StatusOK)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	var req CancelRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}
	resp, derr := s.svc.Cancel(r.PathValue("id"), req)
	writeResult(w, resp, derr, http.StatusOK)
}

func (s *Server) handleRejudge(w http.ResponseWriter, r *http.Request) {
	var req RejudgeRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}
	resp, derr := s.svc.ResolveRejudge(r.PathValue("id"), req)
	writeResult(w, resp, derr, http.StatusOK)
}

func (s *Server) handleAttempts(w http.ResponseWriter, r *http.Request) {
	attempts, derr := s.svc.ListAttempts(r.PathValue("id"))
	if derr != nil {
		writeError(w, derr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attempts": attempts})
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	sum, derr := s.svc.ComputeSummary(r.PathValue("id"))
	if derr != nil {
		writeError(w, derr)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	report, derr := s.svc.GenerateReport(r.PathValue("id"))
	if derr != nil {
		writeError(w, derr)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// decodeBody decodes a JSON request body, mapping malformed input to a stable
// bad-request rejection.
func decodeBody(r *http.Request, dst any) *domain.Error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return domain.NewError(domain.CodeBadRequest, err.Error())
	}
	return nil
}

// writeResult writes a successful response or a domain rejection with the
// appropriate status.
func writeResult(w http.ResponseWriter, resp any, derr *domain.Error, status int) {
	if derr != nil {
		writeError(w, derr)
		return
	}
	writeJSON(w, status, resp)
}

func writeError(w http.ResponseWriter, err error) {
	code := "RICE_INTERNAL"
	reasons := []string{err.Error()}
	if de, ok := err.(*domain.Error); ok {
		code = string(de.Code)
		reasons = de.Reasons
	}
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"error": map[string]any{"code": code, "reasons": reasons},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
