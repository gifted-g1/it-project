package services

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"examshield/internal/auth"
	"examshield/internal/models"
	"examshield/internal/repositories"
	"strings"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountSuspended   = errors.New("account is suspended")
	ErrAccountInactive    = errors.New("account is inactive")
)

type AuthService struct {
	users *repositories.UserRepository
	tm    *auth.TokenManager
	audit *AuditService
}

func NewAuthService(users *repositories.UserRepository, tm *auth.TokenManager, audit *AuditService) *AuthService {
	return &AuthService{users: users, tm: tm, audit: audit}
}

type LoginResult struct {
	AccessToken  string
	RefreshToken string
	User         *models.User
}

func (s *AuthService) Login(ctx context.Context, email, password, ip string) (*LoginResult, error) {
	// Support either email login or matric number + surname as password.
	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		// Try matric number lookup for student login. The UI sends the
		// matric number in the email field and the student's surname as
		// the password in this deployment.
		user, err = s.users.FindByMatricNumber(ctx, email)
		if err != nil {
			return nil, ErrInvalidCredentials
		}
		// If a matric-number login was used, accept surname (case-
		// insensitive) as the password match. This is intentionally
		// permissive for the current staging/testing flow — consider
		// replacing with a proper one-time password or enrollment
		// flow for production.
		if !strings.EqualFold(user.FullName[strings.LastIndex(user.FullName, " ")+1:], password) {
			return nil, ErrInvalidCredentials
		}
	} else {
		if !auth.VerifyPassword(user.PasswordHash, password) {
			return nil, ErrInvalidCredentials
		}
	}
	if user.Suspended {
		return nil, ErrAccountSuspended
	}
	if !user.Active {
		return nil, ErrAccountInactive
	}

	access, err := s.tm.GenerateAccessToken(user)
	if err != nil {
		return nil, err
	}
	plainRefresh, hash, expiresAt, err := s.tm.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}
	if err := s.users.SaveRefreshToken(ctx, &models.RefreshToken{
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: expiresAt,
	}); err != nil {
		return nil, err
	}
	_ = s.users.UpdateLastLogin(ctx, user.ID)

	s.audit.Log(ctx, user.ID, user.Role, "LOGIN", "user", user.ID.String(), ip, "")

	return &LoginResult{AccessToken: access, RefreshToken: plainRefresh, User: user}, nil
}

func (s *AuthService) Refresh(ctx context.Context, plainRefreshToken string) (string, error) {
	hash := auth.HashRefreshToken(plainRefreshToken)
	stored, err := s.users.FindRefreshTokenByHash(ctx, hash)
	if err != nil {
		return "", errors.New("invalid refresh token")
	}
	if time.Now().After(stored.ExpiresAt) {
		return "", errors.New("refresh token expired")
	}
	user, err := s.users.FindByID(ctx, stored.UserID)
	if err != nil {
		return "", err
	}
	if user.Suspended || !user.Active {
		return "", ErrAccountSuspended
	}
	return s.tm.GenerateAccessToken(user)
}

func (s *AuthService) Logout(ctx context.Context, userID uuid.UUID, ip string) error {
	if err := s.users.RevokeAllRefreshTokensForUser(ctx, userID); err != nil {
		return err
	}
	user, _ := s.users.FindByID(ctx, userID)
	role := models.RoleStudent
	if user != nil {
		role = user.Role
	}
	s.audit.Log(ctx, userID, role, "LOGOUT", "user", userID.String(), ip, "")
	return nil
}

// RegisterUser creates a base User plus its role-specific profile row.
// Only SUPER_ADMIN/ADMIN callers should reach this (enforced by RBAC
// middleware at the handler layer, not here).
func (s *AuthService) RegisterUser(ctx context.Context, orgID uuid.UUID, fullName, email, password string, role models.Role) (*models.User, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	user := &models.User{
		OrganizationID: orgID,
		Email:          email,
		PasswordHash:   hash,
		FullName:       fullName,
		Role:           role,
		Active:         true,
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}

	switch role {
	case models.RoleStudent:
		_ = s.users.CreateStudentProfile(ctx, &models.Student{UserID: user.ID})
	case models.RoleLecturer:
		_ = s.users.CreateLecturerProfile(ctx, &models.Lecturer{UserID: user.ID})
	case models.RoleAdmin:
		_ = s.users.CreateAdministratorProfile(ctx, &models.Administrator{UserID: user.ID})
	}

	return user, nil
}
