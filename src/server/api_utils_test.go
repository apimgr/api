package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPNG renders a small placeholder PNG (via the already-verified
// placeholder handler) for use as fixture input to the resize/crop/metadata/
// convert handler tests.
func testPNG(t *testing.T) []byte {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/image/{width}/{height}", apiImagePlaceholderHandler)
	req := httptest.NewRequest(http.MethodGet, "/image/20/20", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	return w.Body.Bytes()
}

// multipartImageRequest builds a multipart/form-data POST request carrying
// the given image bytes under the "image" field plus any extra scalar form
// fields, matching the browser tool-page upload forms.
func multipartImageRequest(t *testing.T, target string, image []byte, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("image", "test.png")
	require.NoError(t, err)
	_, err = fw.Write(image)
	require.NoError(t, err)
	for k, v := range fields {
		require.NoError(t, mw.WriteField(k, v))
	}
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// apiDockerVersionHandler must 400 MISSING_IMAGE with no ?image= and 200
// with a parsed image breakdown when one is supplied.
func TestAPIDockerVersionHandler(t *testing.T) {
	t.Run("missing image", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/docker/version", nil)
		w := httptest.NewRecorder()

		apiDockerVersionHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_IMAGE", env["error"])
	})

	t.Run("valid image", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/docker/version?image=nginx:latest", nil)
		w := httptest.NewRecorder()

		apiDockerVersionHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
	})
}

// apiWeatherCurrentHandler must return 200 with an ok:true envelope for a
// location lookup.
func TestAPIWeatherCurrentHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/weather/{location}", apiWeatherCurrentHandler)

	req := httptest.NewRequest(http.MethodGet, "/weather/London", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, true, env["ok"])
}

// apiGeoIPHandler must 400 IP_LOOKUP_FAILED for an invalid/private IP.
func TestAPIGeoIPHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/geo/{ip}", apiGeoIPHandler)

	req := httptest.NewRequest(http.MethodGet, "/geo/not-an-ip", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, false, env["ok"])
	assert.Equal(t, "IP_LOOKUP_FAILED", env["error"])
}

// apiMathCalculateHandler must dispatch add/divide correctly, reject a
// missing operation, and reject division by zero.
func TestAPIMathCalculateHandler(t *testing.T) {
	t.Run("missing operation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/calculate", nil)
		w := httptest.NewRecorder()

		apiMathCalculateHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_OPERATION", env["error"])
	})

	t.Run("add", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/calculate?operation=add&a=2&b=3", nil)
		w := httptest.NewRecorder()

		apiMathCalculateHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(5), data["result"])
	})

	t.Run("divide by zero", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/calculate?operation=divide&a=1&b=0", nil)
		w := httptest.NewRecorder()

		apiMathCalculateHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "DIVISION_BY_ZERO", env["error"])
	})

	t.Run("unsupported operation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/calculate?operation=frobnicate", nil)
		w := httptest.NewRecorder()

		apiMathCalculateHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "UNSUPPORTED_OPERATION", env["error"])
	})
}

// apiConvertLengthHandler must convert a supported unit pair and reject an
// unsupported one.
func TestAPIConvertLengthHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/convert/{value}/{from}/{to}", apiConvertLengthHandler)

	t.Run("supported pair", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/10/ft/m", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["result"])
	})

	t.Run("unsupported pair", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/10/ft/kg", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "UNSUPPORTED_UNITS", env["error"])
	})

	t.Run("invalid value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/notanumber/ft/m", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_VALUE", env["error"])
	})
}

// apiGenerateQRHandler must always report NOT_SUPPORTED — no QR encoder
// exists anywhere in the codebase.
func TestAPIGenerateQRHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/qr", nil)
	w := httptest.NewRecorder()

	apiGenerateQRHandler(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, "NOT_SUPPORTED", env["error"])
}

// apiValidateEmailHandler must 400 MISSING_EMAIL with no email supplied
// and 200 with a valid:true/false field when one is.
func TestAPIValidateEmailHandler(t *testing.T) {
	t.Run("missing email", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/validate/email", nil)
		w := httptest.NewRecorder()

		apiValidateEmailHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_EMAIL", env["error"])
	})

	t.Run("valid email via query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/validate/email?email=user@example.com", nil)
		w := httptest.NewRecorder()

		apiValidateEmailHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})
}

// apiParseJSONHandler must 400 INVALID_JSON for malformed input and 200
// for a valid document.
func TestAPIParseJSONHandler(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/json", strings.NewReader("{not json"))
		w := httptest.NewRecorder()

		apiParseJSONHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_JSON", env["error"])
	})

	t.Run("valid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/json", strings.NewReader(`{"a":1}`))
		w := httptest.NewRecorder()

		apiParseJSONHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
	})
}

// apiLanguageDetectHandler must always report NOT_SUPPORTED — language
// auto-detection is an IDEA.md non-goal.
func TestAPILanguageDetectHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/language/detect", nil)
	w := httptest.NewRecorder()

	apiLanguageDetectHandler(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, "NOT_SUPPORTED", env["error"])
}

// apiLanguagePhoneticHandler must 400 MISSING_WORD with no ?word= and
// return the known Soundex/Metaphone codes for "Robert" otherwise.
func TestAPILanguagePhoneticHandler(t *testing.T) {
	t.Run("missing word", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/language/phonetic", nil)
		w := httptest.NewRecorder()

		apiLanguagePhoneticHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_WORD", env["error"])
	})

	t.Run("valid word", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/language/phonetic?word=Robert", nil)
		w := httptest.NewRecorder()

		apiLanguagePhoneticHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "R163", data["soundex"])
		assert.Equal(t, "RBRT", data["metaphone"])
	})
}

// apiLanguageWordCountHandler must 400 MISSING_TEXT with no text and return
// correct word/character/line/sentence counts otherwise.
func TestAPILanguageWordCountHandler(t *testing.T) {
	t.Run("missing text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/language/word-count", nil)
		w := httptest.NewRecorder()

		apiLanguageWordCountHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_TEXT", env["error"])
	})

	t.Run("valid text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/language/word-count", strings.NewReader("Hello world. Foo bar!"))
		w := httptest.NewRecorder()

		apiLanguageWordCountHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(4), data["words"])
		assert.Equal(t, float64(1), data["lines"])
		assert.Equal(t, float64(2), data["sentences"])
	})
}

// apiTestHTTPHandler must return 200 with a mock response and a numeric
// duration_ms field.
func TestAPITestHTTPHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test/http", nil)
	w := httptest.NewRecorder()

	apiTestHTTPHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, data["response"])
	assert.NotNil(t, data["duration_ms"])
}

// apiOsintEmailHandler must 400 INVALID_EMAIL for a malformed address and
// 200 for a well-formed one.
func TestAPIOsintEmailHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/osint/{email}", apiOsintEmailHandler)

	t.Run("invalid email", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/osint/not-an-email", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_EMAIL", env["error"])
	})

	t.Run("valid email", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/osint/user@example.com", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "example.com", data["domain"])
	})
}

// apiResearchExtractHandler must always report NOT_SUPPORTED — unstructured
// citation extraction is a confirmed unimplemented gap.
func TestAPIResearchExtractHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/research/extract", nil)
	w := httptest.NewRecorder()

	apiResearchExtractHandler(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, "NOT_SUPPORTED", env["error"])
}

// apiFunJokeHandler must return 200 with type and text fields.
func TestAPIFunJokeHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fun/joke", nil)
	w := httptest.NewRecorder()

	apiFunJokeHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["type"])
	assert.NotEmpty(t, data["text"])
}

// apiLoremPersonHandler must return 200 with a generated person.
func TestAPILoremPersonHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/lorem/person", nil)
	w := httptest.NewRecorder()

	apiLoremPersonHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, true, env["ok"])
}

// apiDevFormatJSONHandler must 400 INVALID_JSON for malformed input and 200
// with a formatted field for valid input.
func TestAPIDevFormatJSONHandler(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/dev/format/json", strings.NewReader("{not json"))
		w := httptest.NewRecorder()

		apiDevFormatJSONHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_JSON", env["error"])
	})

	t.Run("valid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/dev/format/json", strings.NewReader(`{"a":1}`))
		w := httptest.NewRecorder()

		apiDevFormatJSONHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["formatted"])
	})
}

// apiImagePlaceholderHandler must 400 INVALID_WIDTH/INVALID_HEIGHT for
// non-positive dimensions and return raw binary image bytes with the
// matching Content-Type on success.
func TestAPIImagePlaceholderHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/image/{width}/{height}", apiImagePlaceholderHandler)

	t.Run("invalid width", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/image/0/100", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_WIDTH", env["error"])
	})

	t.Run("invalid height", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/image/100/-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_HEIGHT", env["error"])
	})

	t.Run("valid dimensions", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/image/10/10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		assert.True(t, bytes.HasPrefix(w.Body.Bytes(), []byte{0x89, 'P', 'N', 'G'}))
	})
}

// apiImageResizeHandler must 400 on missing image / bad dimensions and
// return a resized image on success, for both multipart uploads and raw
// binary bodies.
func TestAPIImageResizeHandler(t *testing.T) {
	png := testPNG(t)

	t.Run("missing image", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/image/resize?width=10&height=10", nil)
		w := httptest.NewRecorder()

		apiImageResizeHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid width", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/resize", png, map[string]string{"width": "0", "height": "10"})
		w := httptest.NewRecorder()

		apiImageResizeHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_WIDTH", env["error"])
	})

	t.Run("multipart upload", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/resize", png, map[string]string{"width": "10", "height": "10", "format": "png"})
		w := httptest.NewRecorder()

		apiImageResizeHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		assert.True(t, bytes.HasPrefix(w.Body.Bytes(), []byte{0x89, 'P', 'N', 'G'}))
	})

	t.Run("raw body upload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/image/resize?width=10&height=10", bytes.NewReader(png))
		w := httptest.NewRecorder()

		apiImageResizeHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, bytes.HasPrefix(w.Body.Bytes(), []byte{0x89, 'P', 'N', 'G'}))
	})
}

// apiImageCropHandler must 400 on missing image / bad region and return a
// cropped image on success.
func TestAPIImageCropHandler(t *testing.T) {
	png := testPNG(t)

	t.Run("missing image", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/image/crop?x=0&y=0&width=10&height=10", nil)
		w := httptest.NewRecorder()

		apiImageCropHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid width", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/crop", png, map[string]string{"x": "0", "y": "0", "width": "0", "height": "10"})
		w := httptest.NewRecorder()

		apiImageCropHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_WIDTH", env["error"])
	})

	t.Run("multipart upload", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/crop", png, map[string]string{"x": "0", "y": "0", "width": "10", "height": "10", "format": "png"})
		w := httptest.NewRecorder()

		apiImageCropHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		assert.True(t, bytes.HasPrefix(w.Body.Bytes(), []byte{0x89, 'P', 'N', 'G'}))
	})
}

// apiImageMetadataHandler must 400 on missing/undecodable image and return
// width/height/format/size on success.
func TestAPIImageMetadataHandler(t *testing.T) {
	png := testPNG(t)

	t.Run("missing image", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/image/metadata", nil)
		w := httptest.NewRecorder()

		apiImageMetadataHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("undecodable image", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/metadata", []byte("not an image"), nil)
		w := httptest.NewRecorder()

		apiImageMetadataHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "IMAGE_DECODE_FAILED", env["error"])
	})

	t.Run("valid image", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/metadata", png, nil)
		w := httptest.NewRecorder()

		apiImageMetadataHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(20), data["width"])
		assert.Equal(t, float64(20), data["height"])
		assert.Equal(t, "png", data["format"])
	})
}

// apiImageConvertHandler must 400 on missing image / missing format and
// return the re-encoded image on success.
func TestAPIImageConvertHandler(t *testing.T) {
	png := testPNG(t)

	t.Run("missing image", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/image/convert?format=jpeg", nil)
		w := httptest.NewRecorder()

		apiImageConvertHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing format", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/convert", png, nil)
		w := httptest.NewRecorder()

		apiImageConvertHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_FORMAT", env["error"])
	})

	t.Run("convert to jpeg", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/convert", png, map[string]string{"format": "jpeg"})
		w := httptest.NewRecorder()

		apiImageConvertHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))
	})
}
