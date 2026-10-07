package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	appmw "examshield/internal/middleware"
	"examshield/internal/services"
)

type MonitoringHandler struct {
	monitoring *services.MonitoringService
}

func NewMonitoringHandler(monitoring *services.MonitoringService) *MonitoringHandler {
	return &MonitoringHandler{monitoring: monitoring}
}

type verifyFaceRequest struct {
	ExamID      *string `json:"exam_id,omitempty"`
	AttemptID   *string `json:"attempt_id,omitempty"`
	ImageBase64 string  `json:"image_base64"`
	IsReference bool    `json:"is_reference"`
}

// VerifyFace is called by the student client during login/exam entry.
// The image never touches the database directly — Go relays it to the
// Python AI service and stores only the verification outcome.
func (h *MonitoringHandler) VerifyFace(w http.ResponseWriter, r *http.Request) {
	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		appmw.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req verifyFaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ImageBase64 == "" {
		appmw.WriteError(w, http.StatusBadRequest, "image_base64 is required")
		return
	}

	var examID *uuid.UUID
	if req.ExamID != nil {
		if id, err := uuid.Parse(*req.ExamID); err == nil {
			examID = &id
		}
	}
	var attemptID *uuid.UUID
	if req.AttemptID != nil {
		if id, err := uuid.Parse(*req.AttemptID); err == nil {
			attemptID = &id
		}
	}

	result, err := h.monitoring.VerifyFace(r.Context(), examID, attemptID, claims.UserID, req.ImageBase64, req.IsReference)
	if err != nil {
		appmw.WriteError(w, http.StatusBadGateway, "face verification service is unavailable")
		return
	}
	appmw.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"matched":    result.Matched,
		"confidence": result.Confidence,
	})
}

type analyzeFrameRequest struct {
	ExamID      string `json:"exam_id"`
	AttemptID   string `json:"attempt_id"`
	ImageBase64 string `json:"image_base64"`
}

func (h *MonitoringHandler) AnalyzeFrame(w http.ResponseWriter, r *http.Request) {
	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		appmw.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req analyzeFrameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ImageBase64 == "" {
		appmw.WriteError(w, http.StatusBadRequest, "image_base64 is required")
		return
	}
	examID, ok := mustParseUUID(w, req.ExamID)
	if !ok {
		return
	}
	attemptID, ok := mustParseUUID(w, req.AttemptID)
	if !ok {
		return
	}

	events, err := h.monitoring.AnalyzeFrame(r.Context(), examID, attemptID, claims.UserID, req.ImageBase64)
	if err != nil {
		appmw.WriteError(w, http.StatusBadGateway, "AI monitoring service is unavailable")
		return
	}
	appmw.WriteJSON(w, http.StatusOK, events)
}

func (h *MonitoringHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	examID, ok := mustParseUUID(w, mux.Vars(r)["id"])
	if !ok {
		return
	}

	// Route is protected by RBAC middleware (RequireRole(ADMIN, LECTURER))
	// which ensures only authorized users can list exam events.

	events, err := h.monitoring.ListEventsForExam(r.Context(), examID)
	if err != nil {
		appmw.WriteError(w, http.StatusInternalServerError, "unable to list events")
		return
	}
	appmw.WriteJSON(w, http.StatusOK, events)
}

func (h *MonitoringHandler) ReviewEvent(w http.ResponseWriter, r *http.Request) {
	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		appmw.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	eventID, ok := mustParseUUID(w, mux.Vars(r)["eventId"])
	if !ok {
		return
	}
	if err := h.monitoring.ReviewEvent(r.Context(), eventID, claims.UserID); err != nil {
		appmw.WriteError(w, http.StatusInternalServerError, "unable to mark event reviewed")
		return
	}
	appmw.WriteJSON(w, http.StatusOK, map[string]string{"message": "event marked as reviewed"})
}
