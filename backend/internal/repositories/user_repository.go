package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"examshield/internal/models"
)

var ErrNotFound = errors.New("record not found")

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, u *models.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	var u models.User
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &u, err
}

// FindByMatricNumber looks up a student by matric number and returns
// the associated `User` record. Returns ErrNotFound if no match.
func (r *UserRepository) FindByMatricNumber(ctx context.Context, matric string) (*models.User, error) {
	var s models.Student
	err := r.db.WithContext(ctx).Preload("User").Where("matric_number = ?", matric).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &s.User, err
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	var u models.User
	err := r.db.WithContext(ctx).First(&u, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (r *UserRepository) ListByOrganization(ctx context.Context, orgID uuid.UUID, role *models.Role) ([]models.User, error) {
	q := r.db.WithContext(ctx).Where("organization_id = ?", orgID)
	if role != nil {
		q = q.Where("role = ?", *role)
	}
	var users []models.User
	err := q.Order("created_at desc").Find(&users).Error
	return users, err
}

func (r *UserRepository) SetSuspended(ctx context.Context, id uuid.UUID, suspended bool) error {
	return r.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", id).Update("suspended", suspended).Error
}

func (r *UserRepository) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", id).Update("last_login_at", gorm.Expr("now()")).Error
}

func (r *UserRepository) CreateStudentProfile(ctx context.Context, s *models.Student) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *UserRepository) CreateLecturerProfile(ctx context.Context, l *models.Lecturer) error {
	return r.db.WithContext(ctx).Create(l).Error
}

func (r *UserRepository) CreateAdministratorProfile(ctx context.Context, a *models.Administrator) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *UserRepository) SaveRefreshToken(ctx context.Context, t *models.RefreshToken) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *UserRepository) FindRefreshTokenByHash(ctx context.Context, hash string) (*models.RefreshToken, error) {
	var t models.RefreshToken
	err := r.db.WithContext(ctx).Where("token_hash = ? AND revoked = false", hash).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &t, err
}

func (r *UserRepository) RevokeRefreshToken(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&models.RefreshToken{}).Where("id = ?", id).Update("revoked", true).Error
}

func (r *UserRepository) RevokeAllRefreshTokensForUser(ctx context.Context, userID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&models.RefreshToken{}).Where("user_id = ?", userID).Update("revoked", true).Error
}
