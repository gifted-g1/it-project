package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"

	appexam "examshield/internal/exam"
	"examshield/internal/examimport"
	"examshield/internal/models"
	"examshield/internal/repositories"
	"examshield/internal/websocket"
)

var (
	ErrExamNotFound       = errors.New("exam not found")
	ErrInvalidTransition  = errors.New("invalid exam status transition")
	ErrDuplicateAttempt   = errors.New("student already has an attempt for this exam")
	ErrAttemptNotEligible = errors.New("attempt is not eligible for this action")
	ErrNoQuestionsFound   = errors.New("no questions could be parsed from the uploaded PDF")
)

type ExamService struct {
	exams  *repositories.ExamRepository
	mon    *repositories.MonitoringRepository
	hub    *websocket.Hub
	logger *slog.Logger

	// servePort is the port this backend process is itself listening
	// on. LAN mode reuses it — see BuildLANServerInfo — rather than
	// scanning for a separate "available" port that nothing binds a
	// server to.
	servePort int
}

func NewExamService(exams *repositories.ExamRepository, mon *repositories.MonitoringRepository, hub *websocket.Hub, logger *slog.Logger, servePort int) *ExamService {
	return &ExamService{exams: exams, mon: mon, hub: hub, logger: logger, servePort: servePort}
}

func (s *ExamService) CreateExam(ctx context.Context, e *models.Exam) error {
	e.Status = models.ExamStatusDraft
	return s.exams.Create(ctx, e)
}

func (s *ExamService) GetExam(ctx context.Context, id uuid.UUID) (*models.Exam, error) {
	e, err := s.exams.FindByID(ctx, id)
	if errors.Is(err, repositories.ErrNotFound) {
		return nil, ErrExamNotFound
	}
	return e, err
}

func (s *ExamService) UpdateExam(ctx context.Context, e *models.Exam) error {
	return s.exams.Update(ctx, e)
}

func (s *ExamService) DeleteExam(ctx context.Context, id uuid.UUID) error {
	return s.exams.Delete(ctx, id)
}

func (s *ExamService) ListForOrganization(ctx context.Context, orgID uuid.UUID) ([]models.Exam, error) {
	return s.exams.ListByOrganization(ctx, orgID)
}

func (s *ExamService) AddQuestion(ctx context.Context, q *models.Question) error {
	return s.exams.AddQuestion(ctx, q)
}

func (s *ExamService) ListQuestions(ctx context.Context, examID uuid.UUID) ([]models.Question, error) {
	return s.exams.ListQuestions(ctx, examID)
}

func (s *ExamService) DeleteQuestion(ctx context.Context, id uuid.UUID) error {
	return s.exams.DeleteQuestion(ctx, id)
}

// ImportQuestionsFromPDF extracts and parses questions from an
// uploaded PDF (see internal/examimport for the expected layout) and
// creates each one, appended after whatever questions already exist
// on the exam. It returns the created questions so the caller can
// show the lecturer exactly what was imported — parsing is
// best-effort, so results should always be reviewable/editable
// afterward, never silently trusted.
func (s *ExamService) ImportQuestionsFromPDF(ctx context.Context, examID uuid.UUID, pdfReader io.ReaderAt, size int64) ([]models.Question, error) {
	text, err := examimport.ExtractText(pdfReader, size)
	if err != nil {
		return nil, fmt.Errorf("reading PDF: %w", err)
	}

	// Diagnostic: log excerpt and length of extracted text to help
	// understand why some PDFs parse into a single line.
	excerpt := text
	if len(excerpt) > 800 {
		excerpt = excerpt[:800]
	}
	s.logger.Info("pdf extracted", "chars", len(text))
	s.logger.Debug("pdf excerpt", "excerpt", excerpt)

	parsed := examimport.ParseQuestions(text)
	if len(parsed) == 0 {
		return nil, ErrNoQuestionsFound
	}

	s.logger.Info("pdf parsed", "questions_found", len(parsed))
	for i, pq := range parsed {
		// Log each parsed question's type and a short preview for diagnostics
		preview := pq.Text
		if len(preview) > 120 {
			preview = preview[:120]
		}
		s.logger.Debug("parsed question", "index", i, "type", pq.Type, "opts", len(pq.Options), "text_preview", preview)
	}

	existing, err := s.exams.ListQuestions(ctx, examID)
	if err != nil {
		return nil, err
	}
	orderOffset := len(existing)

	var created []models.Question
	for i, pq := range parsed {
		q := &models.Question{
			ExamID:      examID,
			Type:        models.QuestionType(pq.Type),
			Text:        pq.Text,
			Points:      pq.Points,
			OrderIndex:  orderOffset + i,
			CorrectText: pq.CorrectText,
		}
		for _, po := range pq.Options {
			q.Options = append(q.Options, models.Option{Text: po.Text, IsCorrect: po.IsCorrect})
		}
		if err := s.exams.AddQuestion(ctx, q); err != nil {
			s.logger.Error("failed to save an imported question, continuing with the rest", "exam_id", examID, "error", err)
			continue
		}
		created = append(created, *q)
	}

	if len(created) == 0 {
		return nil, ErrNoQuestionsFound
	}
	return created, nil
}

// allowedTransitions encodes the valid exam lifecycle graph so status
// changes can never skip steps (e.g. DRAFT -> ACTIVE directly).
var allowedTransitions = map[models.ExamStatus][]models.ExamStatus{
	models.ExamStatusDraft:     {models.ExamStatusScheduled},
	models.ExamStatusScheduled: {models.ExamStatusPublished, models.ExamStatusDraft},
	models.ExamStatusPublished: {models.ExamStatusActive, models.ExamStatusScheduled},
	models.ExamStatusActive:    {models.ExamStatusEnded},
	models.ExamStatusEnded:     {models.ExamStatusArchived},
}

func (s *ExamService) transition(ctx context.Context, examID uuid.UUID, to models.ExamStatus) (*models.Exam, error) {
	exam, err := s.GetExam(ctx, examID)
	if err != nil {
		return nil, err
	}
	valid := false
	for _, allowed := range allowedTransitions[exam.Status] {
		if allowed == to {
			valid = true
			break
		}
	}
	if !valid {
		return nil, ErrInvalidTransition
	}
	if err := s.exams.UpdateStatus(ctx, examID, to); err != nil {
		return nil, err
	}
	exam.Status = to
	return exam, nil
}

func (s *ExamService) Schedule(ctx context.Context, examID uuid.UUID) (*models.Exam, error) {
	return s.transition(ctx, examID, models.ExamStatusScheduled)
}

func (s *ExamService) Publish(ctx context.Context, examID uuid.UUID) (*models.Exam, error) {
	return s.transition(ctx, examID, models.ExamStatusPublished)
}

// Start transitions the exam to ACTIVE. If the exam is configured for
// LAN mode, it also stands up the local exam server (IP detection,
// free port, QR code) and persists the connection info on the exam.
func (s *ExamService) Start(ctx context.Context, examID uuid.UUID) (*models.Exam, *appexam.LANServerInfo, error) {
	exam, err := s.transition(ctx, examID, models.ExamStatusActive)
	if err != nil {
		return nil, nil, err
	}

	var lanInfo *appexam.LANServerInfo
	if exam.Mode == models.ExamModeLAN {
		lanInfo, err = appexam.BuildLANServerInfo(s.servePort)
		if err != nil {
			s.logger.Error("failed to start LAN exam server", "exam_id", examID, "error", err)
			return exam, nil, err
		}
		if err := s.exams.SetLANServerInfo(ctx, examID, lanInfo.IPAddress, lanInfo.Port); err != nil {
			return exam, lanInfo, err
		}
		exam.LANServerIP = lanInfo.IPAddress
		exam.LANServerPort = lanInfo.Port
	}

	s.hub.Broadcast(examID, websocket.EventExamStatus, map[string]string{"status": string(models.ExamStatusActive)})
	return exam, lanInfo, nil
}

func (s *ExamService) End(ctx context.Context, examID uuid.UUID) (*models.Exam, error) {
	exam, err := s.transition(ctx, examID, models.ExamStatusEnded)
	if err != nil {
		return nil, err
	}
	s.hub.Broadcast(examID, websocket.EventExamStatus, map[string]string{"status": string(models.ExamStatusEnded)})
	return exam, nil
}

// --- Attempts ---

func (s *ExamService) StartAttempt(ctx context.Context, examID, studentID uuid.UUID, ip string) (*models.ExamAttempt, error) {
	exam, err := s.GetExam(ctx, examID)
	if err != nil {
		return nil, err
	}
	if exam.Status != models.ExamStatusActive {
		return nil, ErrAttemptNotEligible
	}

	attempt, err := s.exams.GetOrCreateAttempt(ctx, examID, studentID)
	if err != nil {
		return nil, err
	}
	if attempt.Status == models.AttemptSubmitted {
		return nil, ErrDuplicateAttempt
	}
	if attempt.Status == models.AttemptNotStarted {
		now := nowPtr()
		attempt.Status = models.AttemptInProgress
		attempt.StartedAt = now
		attempt.IPAddress = ip
		if err := s.exams.UpdateAttempt(ctx, attempt); err != nil {
			return nil, err
		}
	}

	s.hub.Broadcast(examID, websocket.EventStudentStarted, map[string]string{"student_id": studentID.String()})
	return attempt, nil
}

// SubmitAttempt finalizes an attempt, auto-grading objective question
// types (multiple choice / true-false) and leaving short-answer
// questions for manual/AI-assisted grading later.
func (s *ExamService) SubmitAttempt(ctx context.Context, attemptID uuid.UUID) (*models.ExamAttempt, error) {
	attempt, err := s.exams.FindAttempt(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.Status == models.AttemptSubmitted {
		return nil, ErrDuplicateAttempt // prevent duplicate submissions server-side
	}

	questions, err := s.exams.ListQuestions(ctx, attempt.ExamID)
	if err != nil {
		return nil, err
	}
	answers, err := s.exams.ListAnswers(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	answerByQuestion := make(map[uuid.UUID]*models.Answer, len(answers))
	for i := range answers {
		answerByQuestion[answers[i].QuestionID] = &answers[i]
	}

	var total float64
	for _, q := range questions {
		ans, ok := answerByQuestion[q.ID]
		if !ok {
			continue
		}
		correct := gradeAnswer(q, ans)
		ans.IsCorrect = &correct
		if correct {
			ans.AwardedPoints = q.Points
			total += q.Points
		}
		_ = s.exams.UpsertAnswer(ctx, ans)
	}

	now := nowPtr()
	attempt.Status = models.AttemptSubmitted
	attempt.SubmittedAt = now
	attempt.Score = &total
	if err := s.exams.UpdateAttempt(ctx, attempt); err != nil {
		return nil, err
	}

	s.hub.Broadcast(attempt.ExamID, websocket.EventStudentSubmitted, map[string]interface{}{
		"student_id": attempt.StudentID.String(),
		"score":      total,
	})
	return attempt, nil
}

// RecordAnswer saves/updates a single answer within an attempt. Final
// grading happens once at SubmitAttempt time so a student can freely
// change answers (where AllowNavigation permits) before submitting.
func (s *ExamService) RecordAnswer(ctx context.Context, ans *models.Answer) error {
	return s.exams.UpsertAnswer(ctx, ans)
}

func gradeAnswer(q models.Question, ans *models.Answer) bool {
	switch q.Type {
	case models.QuestionMultipleChoice:
		if ans.SelectedOptionID == nil {
			return false
		}
		for _, opt := range q.Options {
			if opt.ID == *ans.SelectedOptionID {
				return opt.IsCorrect
			}
		}
		return false
	case models.QuestionTrueFalse:
		return ans.TextAnswer == q.CorrectText
	default:
		// SHORT_ANSWER requires manual/AI-assisted review; never
		// auto-marked correct.
		return false
	}
}

// FindActiveExamForStudent returns the first ACTIVE exam in the
// student's organization, or ErrExamNotFound if none is running.
func (s *ExamService) FindActiveExamForStudent(ctx context.Context, orgID uuid.UUID) (*models.Exam, error) {
	exams, err := s.exams.ListByOrganization(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range exams {
		if exams[i].Status == models.ExamStatusActive {
			return &exams[i], nil
		}
	}
	return nil, ErrExamNotFound
}

// FindAttempt retrieves an exam attempt by ID for authorization checks
// and ownership validation.
func (s *ExamService) FindAttempt(ctx context.Context, attemptID uuid.UUID) (*models.ExamAttempt, error) {
	attempt, err := s.exams.FindAttempt(ctx, attemptID)
	if errors.Is(err, repositories.ErrNotFound) {
		return nil, ErrExamNotFound
	}
	return attempt, err
}
