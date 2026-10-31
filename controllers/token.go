package controllers

import (
	"aunefyren/poenskelisten/auth"
	"aunefyren/poenskelisten/config"
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/logger"
	"aunefyren/poenskelisten/middlewares"
	"aunefyren/poenskelisten/models"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type TokenRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// signInAttempts counts failed sign-in attempts per account: wrong passwords
// under the e-mail typed (whether or not it has an account, so a lock tells
// nothing), and wrong second factors under the user. It complements the
// per-address limit on the routes, which a guesser can dodge by spreading
// attempts over many addresses. initRouter sets it from rate_limit_account.
var signInAttempts = newSignInAttempts(config.AccountRateLimit(models.ConfigStruct{}))

func newSignInAttempts(limit config.RateLimit) *middlewares.AttemptLimiter {
	return middlewares.NewAttemptLimiter(limit.Max, limit.Window)
}

// SetSignInAttemptLimit replaces the per-account limit on failed sign-ins.
// Call it before serving requests.
func SetSignInAttemptLimit(limit config.RateLimit) {
	signInAttempts = newSignInAttempts(limit)
}

func emailAttemptKey(email string) string {
	return "email:" + strings.ToLower(strings.TrimSpace(email))
}

func userAttemptKey(userID uuid.UUID) string {
	return "user:" + userID.String()
}

// dummyPasswordHash is checked against when there's no password to compare
// with, so that a login for an unknown account takes as long as one for a
// real account with a wrong password. It's built on first use, at the cost
// real passwords are hashed with.
var dummyPasswordHash = sync.OnceValue(func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("no account has this password"), models.BcryptCost)
	if err != nil {
		logger.Log.Error("Failed to build the placeholder password hash. Error: " + err.Error())
	}
	return hash
})

// passwordMatches reports whether password is the user's. When there's no
// account (found is false) or it has no local password (OIDC), it still does
// the bcrypt work before saying no, so the response time doesn't tell an
// attacker which e-mail addresses have an account.
func passwordMatches(user models.User, found bool, password string) bool {
	if found && user.HasPassword() {
		return user.CheckPassword(password) == nil
	}
	_ = bcrypt.CompareHashAndPassword(dummyPasswordHash(), []byte(password))
	return false
}

func GenerateToken(context *gin.Context) {
	var request TokenRequest

	err := context.ShouldBindJSON(&request)
	if err != nil {
		logger.Log.Error("Failed to parse request. Error: " + err.Error())
		context.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse request."})
		context.Abort()
		return
	}

	// A locked account is refused before anything is checked, so guesses made
	// during the lock can't be told right from wrong.
	attemptKey := emailAttemptKey(request.Email)
	if locked, retryAfter := signInAttempts.Blocked(attemptKey); locked {
		middlewares.RefuseLockedAttempt(context, retryAfter)
		return
	}

	// check if email exists and password is correct
	user, err := database.GetAllUserInformationByEmail(request.Email)
	found := err == nil
	if err != nil && !errors.Is(err, database.ErrUserNotFound) {
		logger.Log.Error("Failed to get user by e-mail. Error: " + err.Error())
		context.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to log in."})
		context.Abort()
		return
	}

	// An unknown e-mail gets the same answer as a wrong password: the same
	// status, the same message and, through passwordMatches, the same delay.
	if !passwordMatches(user, found, request.Password) {
		logger.Log.Info("Failed login attempt: invalid credentials.")
		if signInAttempts.Failed(attemptKey) {
			// The typed e-mail isn't logged: it's arbitrary client input.
			subject := "an e-mail without an account"
			if found {
				subject = "user " + user.ID.String()
			}
			logger.Log.Warn("Too many failed passwords for " + subject + "; password login for it is locked for a while.")
		}
		context.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials."})
		context.Abort()
		return
	}
	signInAttempts.Succeeded(attemptKey)

	// If MFA is enabled, the password alone is not enough: issue a short-lived
	// challenge token and require the second factor via /open/tokens/mfa instead
	// of handing out a session token here.
	if user.IsMFAEnabled() {
		challengeToken, err := auth.GenerateMFAChallengeToken(user.ID)
		if err != nil {
			logger.Log.Error("Failed to generate MFA challenge token. Error: " + err.Error())
			context.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid credentials."})
			context.Abort()
			return
		}

		context.JSON(http.StatusOK, gin.H{"mfa_required": true, "mfa_token": challengeToken, "message": "Multi-factor authentication required."})
		return
	}

	if err := issueSSOSession(context, user); err != nil {
		logger.Log.Error("Failed to issue session. Error: " + err.Error())
		context.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to log in."})
		context.Abort()
		return
	}

	// The browser is now logged in at the AS; the frontend continues the OAuth
	// authorization flow to obtain tokens. No token is returned here.
	context.JSON(http.StatusOK, gin.H{"message": "Logged in!"})
}
