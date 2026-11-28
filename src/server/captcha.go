package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/database"
)

const captchaLifetime = 10 * time.Minute

// CaptchaDescriptor is the public simple-challenge representation. The
// answer is never included; storage retains only a keyed answer hash.
type CaptchaDescriptor struct {
	Provider  string `json:"provider"`
	CaptchaID string `json:"captcha_id"`
	Question  string `json:"question"`
}

type captchaErrorResponse struct {
	OK    bool `json:"ok"`
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

// AttachSimpleCaptcha creates a fresh simple challenge for a server-rendered
// contact or security-report form and copies only its public fields to the page.
func AttachSimpleCaptcha(ctx context.Context, language string, page *PageData) error {
	if page == nil {
		return fmt.Errorf("page data is required")
	}
	descriptor, err := IssueSimpleCaptcha(ctx, language)
	if err != nil {
		return err
	}
	page.CaptchaID = descriptor.CaptchaID
	page.CaptchaQuestion = descriptor.Question
	return nil
}

// IssueSimpleCaptcha creates a localized arithmetic challenge and stores
// only its keyed answer hash for ten minutes.
func IssueSimpleCaptcha(ctx context.Context, language string) (CaptchaDescriptor, error) {
	left, err := randomCaptchaNumber(20)
	if err != nil {
		return CaptchaDescriptor{}, err
	}
	right, err := randomCaptchaNumber(20)
	if err != nil {
		return CaptchaDescriptor{}, err
	}
	operator := "+"
	answer := left + right
	choice, err := rand.Int(rand.Reader, big.NewInt(2))
	if err != nil {
		return CaptchaDescriptor{}, fmt.Errorf("generate captcha operator: %w", err)
	}
	if choice.Int64() == 1 {
		if right > left {
			left, right = right, left
		}
		operator = "-"
		answer = left - right
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return CaptchaDescriptor{}, fmt.Errorf("generate captcha id: %w", err)
	}
	id := hex.EncodeToString(idBytes)
	question := i18n.TranslateFormat(language, "contact.captcha_question", map[string]string{
		"left": strconv.FormatInt(left, 10), "operator": operator, "right": strconv.FormatInt(right, 10),
	})
	if err := database.StoreCaptchaChallenge(ctx, id, strconv.FormatInt(answer, 10), time.Now().UTC().Add(captchaLifetime)); err != nil {
		return CaptchaDescriptor{}, err
	}
	return CaptchaDescriptor{Provider: "simple", CaptchaID: id, Question: question}, nil
}

// VerifySimpleCaptcha trims the submitted answer and atomically consumes a
// successful, unexpired challenge.
func VerifySimpleCaptcha(ctx context.Context, id, answer string) (bool, error) {
	id = strings.TrimSpace(id)
	answer = strings.TrimSpace(answer)
	if len(id) != 32 || len(answer) == 0 || len(answer) > 3 {
		return false, nil
	}
	for _, char := range answer {
		if char < '0' || char > '9' {
			return false, nil
		}
	}
	return database.VerifyAndConsumeCaptcha(ctx, id, answer, time.Now().UTC())
}

func randomCaptchaNumber(max int64) (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return 0, fmt.Errorf("generate captcha question: %w", err)
	}
	return n.Int64() + 1, nil
}

// simpleCaptchaDescriptorHandler is suitable for the contact API GET route
// and the security-report API descriptor route. The parent router supplies
// the rate-limit middleware as for every other contact request.
// CaptchaDescriptorHandler exposes a newly issued simple challenge for
// callers building the contact and security-report API descriptors.
func CaptchaDescriptorHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		descriptor, err := IssueSimpleCaptcha(r.Context(), i18n.LangFromRequest(r))
		if err != nil {
			writeCaptchaError(w, http.StatusServiceUnavailable, "captcha_unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := writeTaggedJSON(w, responseObject(responseField("captcha", descriptor))); err != nil {
			log.Printf("CAPTCHA descriptor response write failed: %v", err)
		}
	}
}

func writeCaptchaError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	response := captchaErrorResponse{OK: false}
	response.Error.Code = code
	if err := writeTaggedJSON(w, response); err != nil {
		log.Printf("CAPTCHA error response write failed: %v", err)
	}
}
