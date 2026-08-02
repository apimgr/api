package server

import (
	"bytes"
	"encoding/json"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
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

func TestAPIWeatherForecastHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/weather/{location}", apiWeatherForecastHandler)

	t.Run("default days", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/weather/London", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(5), data["days"])
	})

	t.Run("invalid days", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/weather/London?days=notanumber", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_DAYS", env["error"])
	})
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
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
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
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})
}

func TestAPIMathFibonacciHandler(t *testing.T) {
	t.Run("missing count", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/fibonacci", nil)
		w := httptest.NewRecorder()

		apiMathFibonacciHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_COUNT", env["error"])
	})

	t.Run("valid count", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/fibonacci?count=6", nil)
		w := httptest.NewRecorder()

		apiMathFibonacciHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		sequence, ok := data["sequence"].([]interface{})
		require.True(t, ok)
		require.Len(t, sequence, 6)
		assert.Equal(t, []interface{}{"0", "1", "1", "2", "3", "5"}, sequence)
	})

	t.Run("negative count", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/fibonacci?count=-1", nil)
		w := httptest.NewRecorder()

		apiMathFibonacciHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})
}

func TestAPIMathBaseHandler(t *testing.T) {
	t.Run("missing params", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/base", nil)
		w := httptest.NewRecorder()

		apiMathBaseHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("decimal to hex", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/base?number=255&from_base=10&to_base=16", nil)
		w := httptest.NewRecorder()

		apiMathBaseHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "ff", data["result"])
	})

	t.Run("invalid number for base", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/base?number=zz&from_base=10&to_base=16", nil)
		w := httptest.NewRecorder()

		apiMathBaseHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_VALUE", env["error"])
	})
}

func TestAPIMathMatrixHandler(t *testing.T) {
	t.Run("missing matrix", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/math/matrix", strings.NewReader(`{"operation":"add"}`))
		w := httptest.NewRecorder()

		apiMathMatrixHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("add", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/math/matrix", strings.NewReader(`{"operation":"add","a":[[1,2],[3,4]],"b":[[5,6],[7,8]]}`))
		w := httptest.NewRecorder()

		apiMathMatrixHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["result"])
	})

	t.Run("determinant", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/math/matrix", strings.NewReader(`{"operation":"determinant","a":[[1,2],[3,4]]}`))
		w := httptest.NewRecorder()

		apiMathMatrixHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(-2), data["result"])
	})

	t.Run("dimension mismatch", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/math/matrix", strings.NewReader(`{"operation":"add","a":[[1,2]],"b":[[1,2,3]]}`))
		w := httptest.NewRecorder()

		apiMathMatrixHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_MATRIX", env["error"])
	})

	t.Run("unknown operation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/math/matrix", strings.NewReader(`{"operation":"frobnicate","a":[[1]]}`))
		w := httptest.NewRecorder()

		apiMathMatrixHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})
}

func TestAPIMathSequenceHandler(t *testing.T) {
	t.Run("missing params", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/sequence", nil)
		w := httptest.NewRecorder()

		apiMathSequenceHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("arithmetic", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/sequence?type=arithmetic&start=1&step=2&count=5", nil)
		w := httptest.NewRecorder()

		apiMathSequenceHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, []interface{}{float64(1), float64(3), float64(5), float64(7), float64(9)}, data["sequence"])
	})

	t.Run("geometric", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/sequence?type=geometric&start=2&step=2&count=4", nil)
		w := httptest.NewRecorder()

		apiMathSequenceHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, []interface{}{float64(2), float64(4), float64(8), float64(16)}, data["sequence"])
	})

	t.Run("unknown type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/sequence?type=frobnicate&start=1&step=1&count=1", nil)
		w := httptest.NewRecorder()

		apiMathSequenceHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_VALUE", env["error"])
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

func TestAPIConvertAreaHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/convert/{value}/{from}/{to}", apiConvertAreaHandler)

	t.Run("supported pair", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/1/sqm/sqft", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["result"])
	})

	t.Run("unsupported pair", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/1/sqm/kg", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "UNSUPPORTED_UNITS", env["error"])
	})

	t.Run("invalid value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/notanumber/sqm/sqft", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_VALUE", env["error"])
	})
}

func TestAPIConvertDataHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/convert/{value}/{from}/{to}", apiConvertDataHandler)

	t.Run("supported pair", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/1024/b/kb", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["result"])
	})

	t.Run("unsupported pair", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/1/b/sqm", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "UNSUPPORTED_UNITS", env["error"])
	})

	t.Run("invalid value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/notanumber/b/kb", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_VALUE", env["error"])
	})
}

func TestAPIConvertEnergyHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/convert/{value}/{from}/{to}", apiConvertEnergyHandler)

	t.Run("supported pair", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/1/j/cal", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["result"])
	})

	t.Run("unsupported pair", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/1/j/sqm", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "UNSUPPORTED_UNITS", env["error"])
	})

	t.Run("invalid value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/notanumber/j/cal", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_VALUE", env["error"])
	})
}

func TestAPIConvertPressureHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/convert/{value}/{from}/{to}", apiConvertPressureHandler)

	t.Run("supported pair", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/1/pa/bar", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["result"])
	})

	t.Run("unsupported pair", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/1/pa/sqm", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "UNSUPPORTED_UNITS", env["error"])
	})

	t.Run("invalid value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/notanumber/pa/bar", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_VALUE", env["error"])
	})
}

func TestAPIConvertSpeedHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/convert/{value}/{from}/{to}", apiConvertSpeedHandler)

	t.Run("supported pair", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/1/mph/kmh", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["result"])
	})

	t.Run("unsupported pair", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/1/mph/sqm", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "UNSUPPORTED_UNITS", env["error"])
	})

	t.Run("invalid value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/notanumber/mph/kmh", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_VALUE", env["error"])
	})
}

func TestAPIConvertColorHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/convert/color", apiConvertColorHandler)

	t.Run("hex to rgb", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/color?value=%23ff0000&from=hex&to=rgb", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "255,0,0", data["result"])
	})

	t.Run("missing value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/color?from=hex&to=rgb", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("missing format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/color?value=%23ff0000", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("invalid color", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/color?value=notacolor&from=hex&to=rgb", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_COLOR", env["error"])
	})
}

func TestAPIConvertCurrencyHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/convert/currency", apiConvertCurrencyHandler)

	t.Run("missing currency codes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/currency?amount=1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("invalid amount", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/currency?amount=notanumber&from=USD&to=EUR", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_AMOUNT", env["error"])
	})
}

func TestAPIDatetimeFormatHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/datetime/format/{timestamp}/{format}", apiDatetimeFormatHandler)

	t.Run("valid named format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/format/1700000000/iso8601", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["result"])
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/format/notanumber/iso8601", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_TIMESTAMP", env["error"])
	})
}

func TestAPIDatetimeParseHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/datetime/parse/{value}", apiDatetimeParseHandler)

	t.Run("valid date string", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/parse/2024-01-15T10:30:00Z", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["matched_layout"])
	})

	t.Run("unparseable date", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/parse/not-a-date-at-all", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "UNPARSEABLE_DATE", env["error"])
	})
}

func TestAPIDatetimeCronHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/datetime/cron", apiDatetimeCronHandler)

	t.Run("valid expression", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/cron?expression="+url.QueryEscape("*/15 9-17 * * 1-5"), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["next_runs"])
	})

	t.Run("invalid expression", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/cron?expression=bogus", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_CRON", env["error"])
	})
}

func TestAPIDatetimeCalendarHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/datetime/calendar/{year}/{month}", apiDatetimeCalendarHandler)

	t.Run("valid month", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/calendar/2024/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["weeks"])
	})

	t.Run("invalid month", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/calendar/2024/13", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_MONTH", env["error"])
	})
}

func TestAPIDatetimeWorkdaysHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/datetime/workdays/{start}/{end}", apiDatetimeWorkdaysHandler)

	t.Run("valid range", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/workdays/2024-01-01/2024-01-31", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["workdays"])
	})

	t.Run("invalid date", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/workdays/not-a-date/2024-01-31", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_DATE", env["error"])
	})
}

func TestAPIDatetimeSunriseHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/datetime/sunrise/{lat}/{lon}", apiDatetimeSunriseHandler)
	r.Get("/datetime/sunrise/{lat}/{lon}/{date}", apiDatetimeSunriseHandler)

	t.Run("valid coordinates with date", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/sunrise/40.7128/-74.0060/2024-06-21", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["sunrise_utc"])
	})

	t.Run("invalid latitude", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/sunrise/notanumber/-74.0060/2024-06-21", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_LATITUDE", env["error"])
	})
}

func TestAPIDatetimeMoonHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/datetime/moon", apiDatetimeMoonHandler)
	r.Get("/datetime/moon/{date}", apiDatetimeMoonHandler)

	t.Run("valid date", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/moon/2024-06-21", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["phase"])
	})

	t.Run("default no date", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/datetime/moon", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// apiGenerateQRHandler must always report NOT_SUPPORTED — no QR encoder
// exists anywhere in the codebase.
func TestAPIGenerateQRHandler(t *testing.T) {
	t.Run("missing data and ssid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/qr", nil)
		w := httptest.NewRecorder()

		apiGenerateQRHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("plain data renders PNG", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/qr?data=https://example.com", nil)
		w := httptest.NewRecorder()

		apiGenerateQRHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		assert.NotEmpty(t, w.Body.Bytes())
	})

	t.Run("custom width and height respected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/qr?data=hello&width=150&height=150", nil)
		w := httptest.NewRecorder()

		apiGenerateQRHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	})

	t.Run("ssid with WPA security requires password", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/qr?ssid=MyNetwork&security=WPA", nil)
		w := httptest.NewRecorder()

		apiGenerateQRHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_WIFI_QR_PARAMS", env["error"])
	})

	t.Run("ssid with no security defaults to open network PNG", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/qr?ssid=MyNetwork", nil)
		w := httptest.NewRecorder()

		apiGenerateQRHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	})

	t.Run("ssid with password renders PNG", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/qr?ssid=MyNetwork&password=hunter2&security=WPA", nil)
		w := httptest.NewRecorder()

		apiGenerateQRHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		assert.NotEmpty(t, w.Body.Bytes())
	})

	t.Run("ssid with nopass security and no password renders PNG", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/qr?ssid=OpenNetwork&security=nopass&hidden=true", nil)
		w := httptest.NewRecorder()

		apiGenerateQRHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		assert.NotEmpty(t, w.Body.Bytes())
	})
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
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
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

func TestAPIValidateCreditCardHandler(t *testing.T) {
	t.Run("missing number", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/credit-card", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiValidateCreditCardHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid number", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/credit-card", strings.NewReader(`{"number":"4111111111111111"}`))
		w := httptest.NewRecorder()
		apiValidateCreditCardHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})
}

func TestAPIValidateDomainHandler(t *testing.T) {
	t.Run("missing domain", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/domain", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiValidateDomainHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid domain via query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/domain?domain=example.com", nil)
		w := httptest.NewRecorder()
		apiValidateDomainHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})
}

func TestAPIValidateIPHandler(t *testing.T) {
	t.Run("missing ip", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/ip", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiValidateIPHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid ipv4", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/ip", strings.NewReader(`{"ip":"192.168.1.1"}`))
		w := httptest.NewRecorder()
		apiValidateIPHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
		assert.Equal(t, true, data["is_ipv4"])
	})
}

func TestAPIValidateJSONHandler(t *testing.T) {
	t.Run("valid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/json", strings.NewReader(`{"a":1}`))
		w := httptest.NewRecorder()
		apiValidateJSONHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/json", strings.NewReader(`{not json`))
		w := httptest.NewRecorder()
		apiValidateJSONHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, false, data["valid"])
	})
}

func TestAPIValidateMACHandler(t *testing.T) {
	t.Run("missing mac", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/mac", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiValidateMACHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid mac", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/mac", strings.NewReader(`{"mac":"00:1A:2B:3C:4D:5E"}`))
		w := httptest.NewRecorder()
		apiValidateMACHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})
}

func TestAPIValidatePhoneHandler(t *testing.T) {
	t.Run("missing phone", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/phone", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiValidatePhoneHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid phone", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/phone", strings.NewReader(`{"phone":"+14155552671"}`))
		w := httptest.NewRecorder()
		apiValidatePhoneHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})
}

func TestAPIValidateURLHandler(t *testing.T) {
	t.Run("missing url", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/url", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiValidateURLHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid url", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/url", strings.NewReader(`{"url":"https://example.com"}`))
		w := httptest.NewRecorder()
		apiValidateURLHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})
}

func TestAPIValidateUUIDHandler(t *testing.T) {
	t.Run("missing uuid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/uuid", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiValidateUUIDHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid uuid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/uuid", strings.NewReader(`{"uuid":"550e8400-e29b-41d4-a716-446655440000"}`))
		w := httptest.NewRecorder()
		apiValidateUUIDHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})
}

func TestAPIValidateIBANHandler(t *testing.T) {
	t.Run("missing iban", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/iban", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiValidateIBANHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid iban", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/iban", strings.NewReader(`{"iban":"GB29NWBK60161331926819"}`))
		w := httptest.NewRecorder()
		apiValidateIBANHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})

	t.Run("invalid iban checksum", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/iban", strings.NewReader(`{"iban":"GB29NWBK60161331926818"}`))
		w := httptest.NewRecorder()
		apiValidateIBANHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, false, data["valid"])
	})
}

func TestAPIValidateISBNHandler(t *testing.T) {
	t.Run("missing isbn", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/isbn", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiValidateISBNHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid isbn-13", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/isbn", strings.NewReader(`{"isbn":"978-3-16-148410-0"}`))
		w := httptest.NewRecorder()
		apiValidateISBNHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})

	t.Run("valid isbn-10", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/isbn", strings.NewReader(`{"isbn":"0306406152"}`))
		w := httptest.NewRecorder()
		apiValidateISBNHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})

	t.Run("invalid isbn checksum", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/isbn", strings.NewReader(`{"isbn":"978-3-16-148410-1"}`))
		w := httptest.NewRecorder()
		apiValidateISBNHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, false, data["valid"])
	})
}

func TestAPIValidateVATHandler(t *testing.T) {
	t.Run("missing vat", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/vat", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiValidateVATHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid vat format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/vat", strings.NewReader(`{"vat":"GB123456789"}`))
		w := httptest.NewRecorder()
		apiValidateVATHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})

	t.Run("unknown country prefix", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/vat", strings.NewReader(`{"vat":"ZZ123456789"}`))
		w := httptest.NewRecorder()
		apiValidateVATHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, false, data["valid"])
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

// apiParseXMLHandler must 400 MISSING_XML for an empty body and 200 for a
// valid document.
func TestAPIParseXMLHandler(t *testing.T) {
	t.Run("missing xml", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/xml", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiParseXMLHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid xml", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/xml", strings.NewReader("<root><item>value</item></root>"))
		w := httptest.NewRecorder()
		apiParseXMLHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
	})
}

// apiParseCSVHandler must 400 MISSING_CSV for an empty body and 200 with
// one decoded row per data line for valid input.
func TestAPIParseCSVHandler(t *testing.T) {
	t.Run("missing csv", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/csv", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiParseCSVHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid csv", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/csv", strings.NewReader("name,age\nAlice,30\n"))
		w := httptest.NewRecorder()
		apiParseCSVHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].([]interface{})
		require.True(t, ok)
		require.Len(t, data, 1)
		row, ok := data[0].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "Alice", row["name"])
		assert.Equal(t, "30", row["age"])
	})
}

// apiParseJWTHandler must reject a malformed token and decode a well-formed
// one, reusing the exact same decodeJWTSegment logic already covered by the
// crypto-category JWT decoder.
func TestAPIParseJWTHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/parse/jwt/{token}", apiParseJWTHandler)

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/parse/jwt/not-a-jwt", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_JWT", env["error"])
	})

	t.Run("valid token", func(t *testing.T) {
		token := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.signature"
		req := httptest.NewRequest(http.MethodGet, "/parse/jwt/"+token, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		payload, ok := data["payload"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "123", payload["sub"])
	})
}

// apiParseEnvHandler must 400 MISSING_ENV for an empty body and 200 with
// decoded key/value pairs for valid input.
func TestAPIParseEnvHandler(t *testing.T) {
	t.Run("missing env", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/env", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiParseEnvHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid env", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/env", strings.NewReader("# comment\nexport FOO=bar\nBAZ=\"qux\"\n"))
		w := httptest.NewRecorder()
		apiParseEnvHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "bar", data["FOO"])
		assert.Equal(t, "qux", data["BAZ"])
	})
}

// apiParseHTMLHandler must 400 MISSING_HTML for an empty body and 200 with
// a structural summary for valid input.
func TestAPIParseHTMLHandler(t *testing.T) {
	t.Run("missing html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/html", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiParseHTMLHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/html", strings.NewReader("<html><head><title>Example</title></head><body><h1>Hi</h1><a href=\"/x\">x</a></body></html>"))
		w := httptest.NewRecorder()
		apiParseHTMLHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "Example", data["title"])
	})
}

// apiParseINIHandler must 400 MISSING_INI for an empty body and 200 with
// sectioned key/value pairs for valid input.
func TestAPIParseINIHandler(t *testing.T) {
	t.Run("missing ini", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/ini", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiParseINIHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid ini", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/ini", strings.NewReader("[section]\nkey=value\n"))
		w := httptest.NewRecorder()
		apiParseINIHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		section, ok := data["section"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "value", section["key"])
	})
}

// apiParseLogHandler must 400 MISSING_LOG for an empty body and 200 with
// one entry per non-blank line for valid input.
func TestAPIParseLogHandler(t *testing.T) {
	t.Run("missing log", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/log", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiParseLogHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid log", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/log", strings.NewReader("2024-01-02 15:04:05 ERROR something failed\n"))
		w := httptest.NewRecorder()
		apiParseLogHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].([]interface{})
		require.True(t, ok)
		require.Len(t, data, 1)
		entry, ok := data[0].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "ERROR", entry["level"])
	})
}

// apiParseMarkdownHandler must 400 MISSING_MARKDOWN for an empty body and
// 200 with extracted headings/links/code blocks for valid input.
func TestAPIParseMarkdownHandler(t *testing.T) {
	t.Run("missing markdown", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/markdown", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiParseMarkdownHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid markdown", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/markdown", strings.NewReader("# Title\n\n[link](https://example.com)\n\n```go\nfmt.Println(1)\n```\n"))
		w := httptest.NewRecorder()
		apiParseMarkdownHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		headings, ok := data["headings"].([]interface{})
		require.True(t, ok)
		require.Len(t, headings, 1)
		links, ok := data["links"].([]interface{})
		require.True(t, ok)
		require.Len(t, links, 1)
		codeBlocks, ok := data["code_blocks"].([]interface{})
		require.True(t, ok)
		require.Len(t, codeBlocks, 1)
	})
}

// apiParseSQLHandler must 400 MISSING_SQL for an empty body and 200 with a
// best-effort structure summary for valid input; it never rejects on
// invalid SQL syntax.
func TestAPIParseSQLHandler(t *testing.T) {
	t.Run("missing sql", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/sql", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiParseSQLHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid sql", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/sql", strings.NewReader("SELECT id, name FROM users WHERE id = 1"))
		w := httptest.NewRecorder()
		apiParseSQLHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "SELECT", data["statement_type"])
		tables, ok := data["tables"].([]interface{})
		require.True(t, ok)
		require.Contains(t, tables, "users")
	})
}

// apiParseTOMLHandler must 400 MISSING_TOML for an empty body and 200 with
// a decoded nested structure for valid input.
func TestAPIParseTOMLHandler(t *testing.T) {
	t.Run("missing toml", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/toml", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiParseTOMLHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid toml", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/toml", strings.NewReader("[table]\nkey = \"value\"\n"))
		w := httptest.NewRecorder()
		apiParseTOMLHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		table, ok := data["table"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "value", table["key"])
	})
}

// apiParseYAMLHandler must 400 MISSING_YAML for an empty body and 200 with
// a decoded nested structure for valid input.
func TestAPIParseYAMLHandler(t *testing.T) {
	t.Run("missing yaml", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/yaml", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiParseYAMLHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid yaml", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse/yaml", strings.NewReader("key: value\nlist:\n  - one\n  - two\n"))
		w := httptest.NewRecorder()
		apiParseYAMLHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "value", data["key"])
		list, ok := data["list"].([]interface{})
		require.True(t, ok)
		require.Len(t, list, 2)
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

// apiLanguageSpellCheckHandler, apiLanguageGrammarHandler, and
// apiLanguageTranslateHandler must always report NOT_SUPPORTED — spell-check
// and grammar are unnamed in IDEA.md's declared Language scope, and
// translate is an explicit non-goal.
func TestAPILanguageGapHandlers(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{"spell-check", "/api/v1/language/spell-check", apiLanguageSpellCheckHandler},
		{"grammar", "/api/v1/language/grammar", apiLanguageGrammarHandler},
		{"translate", "/api/v1/language/translate", apiLanguageTranslateHandler},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			w := httptest.NewRecorder()

			tc.handler(w, req)

			assert.Equal(t, http.StatusNotImplemented, w.Code)
			env := decodeEnvelope(t, w.Body.Bytes())
			assert.Equal(t, "NOT_SUPPORTED", env["error"])
		})
	}
}

// apiLanguageDictionaryHandler must 400 MISSING_WORD with no ?word=; the
// success path is covered at the service layer in
// src/service/language/language_test.go.
func TestAPILanguageDictionaryHandler(t *testing.T) {
	t.Run("missing word", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/language/dictionary", nil)
		w := httptest.NewRecorder()

		apiLanguageDictionaryHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})
}

// apiLanguageThesaurusHandler must 400 MISSING_WORD with no ?word=; the
// success path is covered at the service layer in
// src/service/language/language_test.go.
func TestAPILanguageThesaurusHandler(t *testing.T) {
	t.Run("missing word", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/language/thesaurus", nil)
		w := httptest.NewRecorder()

		apiLanguageThesaurusHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})
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
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
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
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
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

// apiLanguageKeywordsHandler must 400 MISSING_TEXT with no text and return
// the top non-stopword keywords, most frequent first, otherwise.
func TestAPILanguageKeywordsHandler(t *testing.T) {
	t.Run("missing text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/language/keywords", nil)
		w := httptest.NewRecorder()

		apiLanguageKeywordsHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/language/keywords", strings.NewReader("the cat sat on the mat the cat ran"))
		w := httptest.NewRecorder()

		apiLanguageKeywordsHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		keywords, ok := data["keywords"].([]interface{})
		require.True(t, ok)
		require.NotEmpty(t, keywords)
		top, ok := keywords[0].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "cat", top["word"])
		assert.Equal(t, float64(2), top["count"])
	})

	t.Run("invalid limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/language/keywords?limit=notanumber", strings.NewReader("some text"))
		w := httptest.NewRecorder()

		apiLanguageKeywordsHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_LIMIT", env["error"])
	})
}

// apiLanguageReadabilityHandler must 400 MISSING_TEXT with no text and
// return computed readability scores otherwise.
func TestAPILanguageReadabilityHandler(t *testing.T) {
	t.Run("missing text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/language/readability", nil)
		w := httptest.NewRecorder()

		apiLanguageReadabilityHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/language/readability", strings.NewReader("The cat sat on the mat. The dog ran fast."))
		w := httptest.NewRecorder()

		apiLanguageReadabilityHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(2), data["sentences"])
		assert.NotNil(t, data["flesch_reading_ease"])
		assert.NotNil(t, data["flesch_kincaid_grade"])
		assert.NotNil(t, data["gunning_fog"])
	})
}

// apiLanguageReadingTimeHandler must 400 MISSING_TEXT with no text and
// return an estimated reading time otherwise, honoring ?wpm=.
func TestAPILanguageReadingTimeHandler(t *testing.T) {
	t.Run("missing text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/language/reading-time", nil)
		w := httptest.NewRecorder()

		apiLanguageReadingTimeHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid text with wpm override", func(t *testing.T) {
		text := strings.Repeat("word ", 100)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/language/reading-time?wpm=50", strings.NewReader(text))
		w := httptest.NewRecorder()

		apiLanguageReadingTimeHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(100), data["words"])
		assert.Equal(t, float64(2), data["minutes"])
	})

	t.Run("invalid wpm", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/language/reading-time?wpm=notanumber", strings.NewReader("some text"))
		w := httptest.NewRecorder()

		apiLanguageReadingTimeHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_WPM", env["error"])
	})
}

// apiLanguageSentimentHandler must 400 MISSING_TEXT with no text and return
// a positive/negative/neutral label otherwise.
func TestAPILanguageSentimentHandler(t *testing.T) {
	t.Run("missing text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/language/sentiment", nil)
		w := httptest.NewRecorder()

		apiLanguageSentimentHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("positive text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/language/sentiment", strings.NewReader("This is a good and wonderful day"))
		w := httptest.NewRecorder()

		apiLanguageSentimentHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "positive", data["label"])
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

// apiTestAssertHandler must dispatch each op to the matching test.Service
// Assert* helper and 400 for an unknown op.
func TestAPITestAssertHandler(t *testing.T) {
	t.Run("equal pass", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test/assert", strings.NewReader(`{"op":"equal","expected":"foo","actual":"foo"}`))
		w := httptest.NewRecorder()
		apiTestAssertHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["passed"])
	})

	t.Run("contains missing fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test/assert", strings.NewReader(`{"op":"contains"}`))
		w := httptest.NewRecorder()
		apiTestAssertHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("invalid op", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test/assert", strings.NewReader(`{"op":"bogus"}`))
		w := httptest.NewRecorder()
		apiTestAssertHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})
}

// apiTestFixtureHandler must return the requested fixture type, dispatching
// to test.Service.GenerateFixture.
func TestAPITestFixtureHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/test/fixture/{type}", apiTestFixtureHandler)

	req := httptest.NewRequest(http.MethodGet, "/test/fixture/user", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "user", data["type"])
	assert.NotNil(t, data["fixture"])
}

// apiTestAPIClientHandler must 400 INVALID_BODY when url is missing and
// render curl/javascript/python/go snippets for a valid spec.
func TestAPITestAPIClientHandler(t *testing.T) {
	t.Run("missing url", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test/api-client", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiTestAPIClientHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_BODY", env["error"])
	})

	t.Run("valid spec", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test/api-client", strings.NewReader(`{"method":"POST","url":"https://example.com/api","headers":{"Accept":"application/json"},"body":"{\"a\":1}"}`))
		w := httptest.NewRecorder()
		apiTestAPIClientHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["curl"], "https://example.com/api")
		assert.Contains(t, data["javascript"], "fetch(")
		assert.Contains(t, data["python"], "requests.request")
		assert.Contains(t, data["go"], "http.NewRequest")
	})
}

// apiTestCurlGeneratorHandler must 400 INVALID_BODY when url is missing and
// render a curl command for a valid spec.
func TestAPITestCurlGeneratorHandler(t *testing.T) {
	t.Run("missing url", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test/curl-generator", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiTestCurlGeneratorHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_BODY", env["error"])
	})

	t.Run("valid spec", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test/curl-generator", strings.NewReader(`{"method":"GET","url":"https://example.com/api"}`))
		w := httptest.NewRecorder()
		apiTestCurlGeneratorHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["curl"], "curl -q -LSsf")
		assert.Contains(t, data["curl"], "https://example.com/api")
	})
}

// apiTestPostmanHandler must 400 INVALID_BODY when url is missing and render
// a minimal Postman Collection v2.1 document for a valid spec.
func TestAPITestPostmanHandler(t *testing.T) {
	t.Run("missing url", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test/postman", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiTestPostmanHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_BODY", env["error"])
	})

	t.Run("valid spec", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test/postman", strings.NewReader(`{"method":"GET","url":"https://example.com/api","headers":{"Accept":"application/json"}}`))
		w := httptest.NewRecorder()
		apiTestPostmanHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		info, ok := data["info"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "Generated Request", info["name"])
		items, ok := data["item"].([]interface{})
		require.True(t, ok)
		require.Len(t, items, 1)
	})
}

// apiTestRequestInspectorHandler must echo back method, path, query, and
// headers of the request it receives.
func TestAPITestRequestInspectorHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test/request-inspector?foo=bar", nil)
	req.Header.Set("X-Test", "value")
	w := httptest.NewRecorder()
	apiTestRequestInspectorHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, http.MethodGet, data["method"])
	query, ok := data["query"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "bar", query["foo"])
	headers, ok := data["headers"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "value", headers["X-Test"])
}

// apiTestStatusCodesHandler must return the full table when no code is
// given, a single code's text/description when given a known code, and 400
// INVALID_CODE for an unknown code.
func TestAPITestStatusCodesHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/test/status-codes", apiTestStatusCodesHandler)
	r.Get("/test/status-codes/{code}", apiTestStatusCodesHandler)

	t.Run("full table", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test/status-codes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		codes, ok := data["codes"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, codes)
	})

	t.Run("known code", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test/status-codes/404", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "Not Found", data["text"])
	})

	t.Run("unknown code", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test/status-codes/999", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_CODE", env["error"])
	})
}

// apiTestResponseGeneratorHandler must dispatch directly to
// test.Service.GenerateMockAPIResponse.
func TestAPITestResponseGeneratorHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test/response-generator", nil)
	w := httptest.NewRecorder()
	apiTestResponseGeneratorHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.NotNil(t, env["data"])
}

// apiTestWebhookHandler must echo back headers and parsed/raw body for a
// posted JSON payload, and report json_valid=false for a non-JSON body.
func TestAPITestWebhookHandler(t *testing.T) {
	t.Run("valid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test/webhook", strings.NewReader(`{"event":"example"}`))
		req.Header.Set("X-Hub-Signature", "abc123")
		w := httptest.NewRecorder()
		apiTestWebhookHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["json_valid"])
		headers, ok := data["headers"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "abc123", headers["X-Hub-Signature"])
	})

	t.Run("non-json body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test/webhook", strings.NewReader(`not json`))
		w := httptest.NewRecorder()
		apiTestWebhookHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, false, data["json_valid"])
		assert.Equal(t, "not json", data["raw_body"])
	})
}

// apiTestLoadTestHandler and apiTestMockServerHandler are permanent gaps and
// must always 501 NOT_SUPPORTED, matching the TestAPIResearchGapHandlers
// pattern.
func TestAPITestPermanentGapHandlers(t *testing.T) {
	handlers := map[string]http.HandlerFunc{
		"load-test":   apiTestLoadTestHandler,
		"mock-server": apiTestMockServerHandler,
	}
	for tool, handler := range handlers {
		t.Run(tool, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/test/"+tool, nil)
			w := httptest.NewRecorder()

			handler(w, req)

			assert.Equal(t, http.StatusNotImplemented, w.Code)
			env := decodeEnvelope(t, w.Body.Bytes())
			assert.Equal(t, "NOT_SUPPORTED", env["error"])
		})
	}
}

// apiTestFakeDataHandler must default to type=user and 400 INVALID_TYPE for
// an unknown type.
func TestAPITestFakeDataHandler(t *testing.T) {
	t.Run("default user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/test/fake-data", nil)
		w := httptest.NewRecorder()
		apiTestFakeDataHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "user", data["type"])
		assert.NotNil(t, data["user"])
	})

	t.Run("email type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/test/fake-data?type=email&prefix=qa", nil)
		w := httptest.NewRecorder()
		apiTestFakeDataHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["email"], "qa+test")
	})

	t.Run("invalid type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/test/fake-data?type=bogus", nil)
		w := httptest.NewRecorder()
		apiTestFakeDataHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})
}

// apiWeatherMapsHandler and apiWeatherRadarHandler are permanent gaps and
// must always 501 NOT_SUPPORTED, matching the TestAPITestPermanentGapHandlers
// pattern.
func TestAPIWeatherPermanentGapHandlers(t *testing.T) {
	handlers := map[string]http.HandlerFunc{
		"maps":  apiWeatherMapsHandler,
		"radar": apiWeatherRadarHandler,
	}
	for tool, handler := range handlers {
		t.Run(tool, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/"+tool+"/London", nil)
			w := httptest.NewRecorder()

			handler(w, req)

			assert.Equal(t, http.StatusNotImplemented, w.Code)
			env := decodeEnvelope(t, w.Body.Bytes())
			assert.Equal(t, "NOT_SUPPORTED", env["error"])
		})
	}
}

// apiWeatherAirQualityHandler must return 200 with an ok:true envelope for a
// location lookup.
func TestAPIWeatherAirQualityHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/weather/{location}", apiWeatherAirQualityHandler)

	req := httptest.NewRequest(http.MethodGet, "/weather/London", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, true, env["ok"])
}

// apiWeatherAlertsHandler must return 200 with a location/alerts envelope.
func TestAPIWeatherAlertsHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/weather/{location}", apiWeatherAlertsHandler)

	req := httptest.NewRequest(http.MethodGet, "/weather/London", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, true, env["ok"])
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "London", data["location"])
	assert.NotNil(t, data["alerts"])
}

// apiWeatherAstronomyHandler must return 200 with an ok:true envelope for a
// location lookup.
func TestAPIWeatherAstronomyHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/weather/{location}", apiWeatherAstronomyHandler)

	req := httptest.NewRequest(http.MethodGet, "/weather/London", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, true, env["ok"])
}

// apiWeatherHistoricalHandler must 400 MISSING_DATE_RANGE with no ?start=/
// ?end= and 200 with a days breakdown when both are supplied.
func TestAPIWeatherHistoricalHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/weather/{location}", apiWeatherHistoricalHandler)

	t.Run("missing date range", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/weather/London", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid date range", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/weather/London?start=2024-01-01&end=2024-01-02", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "2024-01-01", data["start"])
		assert.Equal(t, "2024-01-02", data["end"])
	})
}

// apiWeatherHourlyHandler must default to 24 hours, 400 INVALID_HOURS for a
// non-integer ?hours=, and 200 with an hours breakdown otherwise.
func TestAPIWeatherHourlyHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/weather/{location}", apiWeatherHourlyHandler)

	t.Run("default hours", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/weather/London", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(24), data["hours"])
	})

	t.Run("invalid hours", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/weather/London?hours=notanumber", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_HOURS", env["error"])
	})
}

// apiWeatherMarineHandler must return 200 with an ok:true envelope for a
// location lookup.
func TestAPIWeatherMarineHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/weather/{location}", apiWeatherMarineHandler)

	req := httptest.NewRequest(http.MethodGet, "/weather/Miami", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, true, env["ok"])
}

// apiWeatherPollenHandler must return 200 with an ok:true envelope for a
// location lookup.
func TestAPIWeatherPollenHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/weather/{location}", apiWeatherPollenHandler)

	req := httptest.NewRequest(http.MethodGet, "/weather/Berlin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, true, env["ok"])
}

// apiWeatherUVHandler must return 200 with an ok:true envelope for a
// location lookup.
func TestAPIWeatherUVHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/weather/{location}", apiWeatherUVHandler)

	req := httptest.NewRequest(http.MethodGet, "/weather/London", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, true, env["ok"])
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
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
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

// apiOsintDomainHandler must 400 MISSING_DOMAIN when the {domain} path
// parameter is empty. A successful-lookup case is intentionally not
// asserted here (like TestAPIGeoIPHandler) since WHOISLookup performs a
// live network call that would make CI flaky.
func TestAPIOsintDomainHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/osint/domain/", nil)
	w := httptest.NewRecorder()

	apiOsintDomainHandler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, "VALIDATION_FAILED", env["error"])
}

// apiOsintIPHandler must reject an invalid IP address. It reuses the same
// osintService.IPLookup as apiGeoIPHandler, so it is tested the same way.
func TestAPIOsintIPHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/osint/ip/{ip}", apiOsintIPHandler)

	req := httptest.NewRequest(http.MethodGet, "/osint/ip/not-an-ip", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, "IP_LOOKUP_FAILED", env["error"])
}

// apiOsintCertHandler must 400 MISSING_DOMAIN when the {domain} path
// parameter is empty. A successful-lookup case is intentionally not
// asserted here since SSLInfo performs a live TLS handshake that would
// make CI flaky.
func TestAPIOsintCertHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/osint/cert/", nil)
	w := httptest.NewRecorder()

	apiOsintCertHandler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, "VALIDATION_FAILED", env["error"])
}

// apiOsintSubdomainHandler must 400 MISSING_DOMAIN when the {domain} path
// parameter is empty. A successful-lookup case is intentionally not
// asserted here since SubdomainEnum performs live DNS resolution that would
// make CI flaky (same reasoning as TestAPIOsintCertHandler).
func TestAPIOsintSubdomainHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/osint/subdomain/", nil)
	w := httptest.NewRecorder()

	apiOsintSubdomainHandler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, "VALIDATION_FAILED", env["error"])
}

// apiOsintTechStackHandler must 400 MISSING_URL when ?url= is absent. A
// successful-lookup case is intentionally not asserted here since TechStack
// performs a live HTTP request that would make CI flaky (same reasoning as
// TestAPIOsintCertHandler).
func TestAPIOsintTechStackHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/osint/tech-stack", nil)
	w := httptest.NewRecorder()

	apiOsintTechStackHandler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, "VALIDATION_FAILED", env["error"])
}

// apiOsintBreachHandler, apiOsintCompanyHandler, apiOsintMetadataHandler,
// apiOsintPhoneHandler, apiOsintSocialHandler, and apiOsintUsernameHandler
// all require either a keyed/paid third-party API or a much larger outbound
// surface than IDEA.md's declared free/keyless OSINT scope — they must
// always report NOT_SUPPORTED.
func TestAPIOsintGapHandlers(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{"breach", "/api/v1/osint/breach/user@example.com", apiOsintBreachHandler},
		{"company", "/api/v1/osint/company/example", apiOsintCompanyHandler},
		{"metadata", "/api/v1/osint/metadata/file", apiOsintMetadataHandler},
		{"phone", "/api/v1/osint/phone/+15555550100", apiOsintPhoneHandler},
		{"social", "/api/v1/osint/social/example", apiOsintSocialHandler},
		{"username", "/api/v1/osint/username/example", apiOsintUsernameHandler},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()

			tc.handler(w, req)

			assert.Equal(t, http.StatusNotImplemented, w.Code)
			env := decodeEnvelope(t, w.Body.Bytes())
			assert.Equal(t, "NOT_SUPPORTED", env["error"])
		})
	}
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

// The remaining research sub-tools (bibtex, footnotes, metadata, outline,
// pdf-extract, readability, scraper, summarize) are permanent gaps per
// TODO.AI.md's "Known permanent API gaps" section: none are named in
// IDEA.md's declared Research scope, and several would add a new
// outbound-call family or third-party dependency beyond that scope. Each
// must 501 NOT_SUPPORTED.
func TestAPIResearchGapHandlers(t *testing.T) {
	handlers := map[string]http.HandlerFunc{
		"bibtex":      apiResearchBibtexHandler,
		"footnotes":   apiResearchFootnotesHandler,
		"metadata":    apiResearchMetadataHandler,
		"outline":     apiResearchOutlineHandler,
		"pdf-extract": apiResearchPdfExtractHandler,
		"readability": apiResearchReadabilityHandler,
		"scraper":     apiResearchScraperHandler,
		"summarize":   apiResearchSummarizeHandler,
	}
	for tool, handler := range handlers {
		t.Run(tool, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/research/"+tool, nil)
			w := httptest.NewRecorder()

			handler(w, req)

			assert.Equal(t, http.StatusNotImplemented, w.Code)
			env := decodeEnvelope(t, w.Body.Bytes())
			assert.Equal(t, "NOT_SUPPORTED", env["error"])
		})
	}
}

// apiResearchArxivHandler must 400 MISSING_ID with no id (JSON body or
// ?id= query parameter); the success path is covered at the service layer
// in src/service/research/research_test.go.
func TestAPIResearchArxivHandler(t *testing.T) {
	t.Run("missing id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/research/arxiv", nil)
		w := httptest.NewRecorder()

		apiResearchArxivHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})
}

// apiResearchIsbnHandler must 400 MISSING_ISBN with no isbn (JSON body or
// ?isbn= query parameter); the success path is covered at the service layer
// in src/service/research/research_test.go.
func TestAPIResearchIsbnHandler(t *testing.T) {
	t.Run("missing isbn", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/research/isbn", nil)
		w := httptest.NewRecorder()

		apiResearchIsbnHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})
}

// apiResearchCitationHandler must 400 MISSING_FIELDS when title/author are
// absent, default to APA style when none is given, and honor an explicit
// style.
func TestAPIResearchCitationHandler(t *testing.T) {
	t.Run("missing fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/research/citation", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiResearchCitationHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("default style", func(t *testing.T) {
		body := `{"title":"On the Origin of Species","author":"Darwin, C.","year":"1859","source":"John Murray"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/research/citation", strings.NewReader(body))
		w := httptest.NewRecorder()
		apiResearchCitationHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "APA", data["style"])
		assert.Contains(t, data["citation"], "Darwin, C.")
	})

	t.Run("mla style", func(t *testing.T) {
		body := `{"title":"On the Origin of Species","author":"Darwin, C.","year":"1859","source":"John Murray","style":"MLA"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/research/citation", strings.NewReader(body))
		w := httptest.NewRecorder()
		apiResearchCitationHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "MLA", data["style"])
		assert.Contains(t, data["citation"], "\"On the Origin of Species.\"")
	})
}

// apiResearchDOIHandler must 400 INVALID_DOI for a malformed DOI and 200
// with the canonical resolver URL for a well-formed one, including a DOI
// containing a "/" (the normal case, requiring the wildcard route).
func TestAPIResearchDOIHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/research/doi/*", apiResearchDOIHandler)

	t.Run("invalid doi", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/research/doi/not-a-doi", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_DOI", env["error"])
	})

	t.Run("valid doi", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/research/doi/10.1000/182", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "10.1000/182", data["doi"])
		assert.Equal(t, "https://doi.org/10.1000/182", data["url"])
	})
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

// apiFunDadJokeHandler must return 200 with a non-empty text field.
func TestAPIFunDadJokeHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fun/dad-joke", nil)
	w := httptest.NewRecorder()

	apiFunDadJokeHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["text"])
}

// apiFunProgrammingJokeHandler must return 200 with a non-empty text field.
func TestAPIFunProgrammingJokeHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fun/programming-joke", nil)
	w := httptest.NewRecorder()

	apiFunProgrammingJokeHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["text"])
}

// apiFunQuoteHandler must return 200 with a non-empty text field.
func TestAPIFunQuoteHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fun/quote", nil)
	w := httptest.NewRecorder()

	apiFunQuoteHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["text"])
}

// apiFunFactHandler must return 200 with a non-empty text field.
func TestAPIFunFactHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fun/fact", nil)
	w := httptest.NewRecorder()

	apiFunFactHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["text"])
}

// apiFunMotivationalHandler must return 200 with a non-empty text field.
func TestAPIFunMotivationalHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fun/motivational", nil)
	w := httptest.NewRecorder()

	apiFunMotivationalHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["text"])
}

// apiFunInsultHandler must return 200 with a non-empty text field.
func TestAPIFunInsultHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fun/insult", nil)
	w := httptest.NewRecorder()

	apiFunInsultHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["text"])
}

// apiFunComplimentHandler must return 200 with a non-empty text field.
func TestAPIFunComplimentHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fun/compliment", nil)
	w := httptest.NewRecorder()

	apiFunComplimentHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["text"])
}

// apiFunMemeHandler must return 200 with a non-empty text field.
func TestAPIFunMemeHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fun/meme", nil)
	w := httptest.NewRecorder()

	apiFunMemeHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["text"])
}

// apiFunRiddleHandler must return 200 with non-empty question and answer fields.
func TestAPIFunRiddleHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fun/riddle", nil)
	w := httptest.NewRecorder()

	apiFunRiddleHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["question"])
	assert.NotEmpty(t, data["answer"])
}

// apiFunTriviaHandler must return 200 with non-empty question and answer fields.
func TestAPIFunTriviaHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fun/trivia", nil)
	w := httptest.NewRecorder()

	apiFunTriviaHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["question"])
	assert.NotEmpty(t, data["answer"])
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

// apiDevCronHandler reuses datetime.ParseCron and must behave identically
// to apiDatetimeCronHandler.
func TestAPIDevCronHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/dev/cron", apiDevCronHandler)

	t.Run("valid expression", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dev/cron?expression="+url.QueryEscape("*/15 9-17 * * 1-5"), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["next_runs"])
	})

	t.Run("invalid expression", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dev/cron?expression=bogus", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_CRON", env["error"])
	})
}

// apiDevJWTHandler reuses decodeJWTSegment and must decode (never verify)
// the header and payload of a JWT, matching apiParseJWTHandler's behavior.
func TestAPIDevJWTHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/dev/jwt/{token}", apiDevJWTHandler)

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dev/jwt/not-a-jwt", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_JWT", env["error"])
	})

	t.Run("valid token", func(t *testing.T) {
		token := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.signature"
		req := httptest.NewRequest(http.MethodGet, "/dev/jwt/"+token, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "signature", data["signature"])
		header, ok := data["header"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "HS256", header["alg"])
	})
}

// apiDevEchoHandler must reflect back method, path, query, headers,
// remote address, and body with no external service dependency.
func TestAPIDevEchoHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dev/echo?foo=bar", strings.NewReader("hello"))
	req.Header.Set("X-Custom-Header", "test-value")
	w := httptest.NewRecorder()

	apiDevEchoHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, http.MethodGet, data["method"])
	assert.Equal(t, "/api/v1/dev/echo", data["path"])
	assert.Equal(t, "hello", data["body"])
	query, ok := data["query"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "bar", query["foo"])
	headers, ok := data["headers"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "test-value", headers["X-Custom-Header"])
}

// apiDevFormatCSSHandler must format by default and minify with
// ?minify=true.
func TestAPIDevFormatCSSHandler(t *testing.T) {
	t.Run("format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/dev/format/css", strings.NewReader("body{color:red;}"))
		w := httptest.NewRecorder()
		apiDevFormatCSSHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["formatted"], "\n")
	})

	t.Run("minify", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/dev/format/css?minify=true", strings.NewReader("body {\n  color: red;\n}"))
		w := httptest.NewRecorder()
		apiDevFormatCSSHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "body{color:red;}", data["formatted"])
	})
}

// apiDevFormatHTMLHandler must format by default and minify with
// ?minify=true.
func TestAPIDevFormatHTMLHandler(t *testing.T) {
	t.Run("format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/dev/format/html", strings.NewReader("<div><p>Hello</p></div>"))
		w := httptest.NewRecorder()
		apiDevFormatHTMLHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["formatted"], "\n")
	})

	t.Run("minify", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/dev/format/html?minify=true", strings.NewReader("<div>\n  <p>Hello</p>\n</div>"))
		w := httptest.NewRecorder()
		apiDevFormatHTMLHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "<div><p>Hello</p></div>", data["formatted"])
	})
}

// apiDevFormatJSHandler must format by default and minify with
// ?minify=true.
func TestAPIDevFormatJSHandler(t *testing.T) {
	t.Run("format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/dev/format/js", strings.NewReader("function greet(){console.log('hi');}"))
		w := httptest.NewRecorder()
		apiDevFormatJSHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["formatted"], "\n")
	})

	t.Run("minify", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/dev/format/js?minify=true", strings.NewReader("function greet() {\n  console.log('hi');\n}"))
		w := httptest.NewRecorder()
		apiDevFormatJSHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotContains(t, data["formatted"], "\n")
	})
}

// apiDevFormatSQLHandler has no minify variant — a formatted SQL query is
// the tool's only mode.
func TestAPIDevFormatSQLHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dev/format/sql", strings.NewReader("select * from users where id=1 and active=1"))
	w := httptest.NewRecorder()
	apiDevFormatSQLHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, data["formatted"], "\nWHERE")
}

// apiDevFormatXMLHandler must format by default, minify with
// ?minify=true, and 400 INVALID_XML on malformed input.
func TestAPIDevFormatXMLHandler(t *testing.T) {
	t.Run("format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/dev/format/xml", strings.NewReader("<root><item>value</item></root>"))
		w := httptest.NewRecorder()
		apiDevFormatXMLHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["formatted"], "\n")
	})

	t.Run("minify", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/dev/format/xml?minify=true", strings.NewReader("<root>\n  <item>value</item>\n</root>"))
		w := httptest.NewRecorder()
		apiDevFormatXMLHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "<root><item>value</item></root>", data["formatted"])
	})

	t.Run("invalid xml", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/dev/format/xml", strings.NewReader("<root><item></root>"))
		w := httptest.NewRecorder()
		apiDevFormatXMLHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_XML", env["error"])
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
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("invalid height", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/image/100/-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
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
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
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
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
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
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("convert to jpeg", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/convert", png, map[string]string{"format": "jpeg"})
		w := httptest.NewRecorder()

		apiImageConvertHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))
	})
}

// apiImageAvatarHandler is a thin wrapper around generateService.Avatar; it
// must behave identically to apiGenerateAvatarHandler.
func TestAPIImageAvatarHandler(t *testing.T) {
	t.Run("missing initials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/image/avatar", nil)
		w := httptest.NewRecorder()
		apiImageAvatarHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid initials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/image/avatar?initials=AB&size=64", nil)
		w := httptest.NewRecorder()
		apiImageAvatarHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		img, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		require.NoError(t, err)
		assert.Equal(t, 64, img.Bounds().Dx())
	})
}

// apiImageBarcodeHandler is a thin wrapper around generateService.Barcode;
// it must behave identically to apiGenerateBarcodeHandler.
func TestAPIImageBarcodeHandler(t *testing.T) {
	t.Run("missing data", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/image/barcode?format=code128", nil)
		w := httptest.NewRecorder()
		apiImageBarcodeHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid code128", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/image/barcode?format=code128&data=Hello123", nil)
		w := httptest.NewRecorder()
		apiImageBarcodeHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		_, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		require.NoError(t, err)
	})

	t.Run("unsupported format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/image/barcode?format=nope&data=abc", nil)
		w := httptest.NewRecorder()
		apiImageBarcodeHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "BARCODE_GENERATION_FAILED", env["error"])
	})
}

// apiImageIdenticonHandler is a thin wrapper around generateService.Identicon;
// it must behave identically to apiGenerateIdenticonHandler.
func TestAPIImageIdenticonHandler(t *testing.T) {
	t.Run("missing seed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/image/identicon", nil)
		w := httptest.NewRecorder()
		apiImageIdenticonHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid seed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/image/identicon?seed=user@example.com", nil)
		w := httptest.NewRecorder()
		apiImageIdenticonHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		_, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		require.NoError(t, err)
	})
}

// apiImageQRHandler is a permanent API gap: no QR encoder exists anywhere
// in the codebase, so it must always report the same honest 501 as
// apiGenerateQRHandler.
func TestAPIImageQRHandler(t *testing.T) {
	t.Run("missing data and ssid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/image/qr", nil)
		w := httptest.NewRecorder()
		apiImageQRHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("plain data renders PNG", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/image/qr?data=https://example.com", nil)
		w := httptest.NewRecorder()
		apiImageQRHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		assert.NotEmpty(t, w.Body.Bytes())
	})
}

// apiImageFilterHandler must 400 on missing image / missing filter name,
// error on an unknown filter name, and return the filtered image on
// success.
func TestAPIImageFilterHandler(t *testing.T) {
	fixture := testPNG(t)

	t.Run("missing image", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/image/filter?name=grayscale", nil)
		w := httptest.NewRecorder()

		apiImageFilterHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing filter name", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/filter", fixture, nil)
		w := httptest.NewRecorder()

		apiImageFilterHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("unknown filter", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/filter", fixture, map[string]string{"name": "does-not-exist"})
		w := httptest.NewRecorder()

		apiImageFilterHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "IMAGE_FILTER_FAILED", env["error"])
	})

	t.Run("grayscale filter", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/filter", fixture, map[string]string{"name": "grayscale"})
		w := httptest.NewRecorder()

		apiImageFilterHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		_, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		require.NoError(t, err)
	})
}

// apiImageOptimizeHandler must 400 on missing image, fall back to a
// default JPEG quality, and report original/optimized sizes via headers.
func TestAPIImageOptimizeHandler(t *testing.T) {
	fixture := testPNG(t)

	t.Run("missing image", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/image/optimize?format=jpeg", nil)
		w := httptest.NewRecorder()

		apiImageOptimizeHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("optimize to jpeg", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/optimize", fixture, map[string]string{"format": "jpeg", "quality": "40"})
		w := httptest.NewRecorder()

		apiImageOptimizeHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))
		assert.NotEmpty(t, w.Header().Get("X-Original-Size"))
		assert.NotEmpty(t, w.Header().Get("X-Optimized-Size"))
	})

	t.Run("invalid quality falls back to default", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/optimize", fixture, map[string]string{"format": "jpeg", "quality": "0"})
		w := httptest.NewRecorder()

		apiImageOptimizeHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// apiImageWatermarkHandler must 400 on missing image / missing text and
// return the watermarked image on success.
func TestAPIImageWatermarkHandler(t *testing.T) {
	fixture := testPNG(t)

	t.Run("missing image", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/image/watermark?text=hello", nil)
		w := httptest.NewRecorder()

		apiImageWatermarkHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing text", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/watermark", fixture, nil)
		w := httptest.NewRecorder()

		apiImageWatermarkHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid text", func(t *testing.T) {
		req := multipartImageRequest(t, "/api/v1/image/watermark", fixture, map[string]string{"text": "hello", "opacity": "0.5"})
		w := httptest.NewRecorder()

		apiImageWatermarkHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		_, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		require.NoError(t, err)
	})
}

// apiTextCompressHandler must 400 MISSING_DATA when data is absent, default
// to gzip compress mode, and round-trip through decompress.
func TestAPITextCompressHandler(t *testing.T) {
	t.Run("missing data", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/compress", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiTextCompressHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("round trip gzip", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/compress", strings.NewReader(`{"data":"hello world"}`))
		w := httptest.NewRecorder()
		apiTextCompressHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		compressed := data["result"].(string)

		body, err := json.Marshal(map[string]string{"data": compressed, "mode": "decompress"})
		require.NoError(t, err)
		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/text/compress", bytes.NewReader(body))
		w2 := httptest.NewRecorder()
		apiTextCompressHandler(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Code)
		env2 := decodeEnvelope(t, w2.Body.Bytes())
		data2 := env2["data"].(map[string]interface{})
		assert.Equal(t, "hello world", data2["result"])
	})

	t.Run("invalid mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/compress", strings.NewReader(`{"data":"x","mode":"bogus"}`))
		w := httptest.NewRecorder()
		apiTextCompressHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})
}

// apiTextDiffHandler must return a non-empty diff for differing inputs.
func TestAPITextDiffHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/text/diff", strings.NewReader(`{"text1":"a\nb","text2":"a\nc"}`))
	w := httptest.NewRecorder()

	apiTextDiffHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["diff"])
}

// apiTextExtractHandler must default to emails and support the other three
// extraction types, and 400 on an unknown type.
func TestAPITextExtractHandler(t *testing.T) {
	t.Run("default emails", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/extract", strings.NewReader(`{"text":"reach me at a@example.com"}`))
		w := httptest.NewRecorder()
		apiTextExtractHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		matches, ok := data["matches"].([]interface{})
		require.True(t, ok)
		assert.Contains(t, matches, "a@example.com")
	})

	t.Run("urls", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/extract", strings.NewReader(`{"text":"visit https://example.com now","type":"urls"}`))
		w := httptest.NewRecorder()
		apiTextExtractHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/extract", strings.NewReader(`{"text":"x","type":"bogus"}`))
		w := httptest.NewRecorder()
		apiTextExtractHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})
}

// apiTextNanoIDHandler and apiTextULIDHandler must return non-empty
// generated IDs.
func TestAPITextNanoIDAndULIDHandlers(t *testing.T) {
	t.Run("nanoid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/text/nanoid", nil)
		w := httptest.NewRecorder()
		apiTextNanoIDHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["nanoid"])
	})

	t.Run("ulid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/text/ulid", nil)
		w := httptest.NewRecorder()
		apiTextULIDHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["ulid"])
	})
}

// apiTextRegexHandler must 400 MISSING_PATTERN when pattern is absent, and
// support match, replace, and explain modes.
func TestAPITextRegexHandler(t *testing.T) {
	t.Run("missing pattern", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/regex", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiTextRegexHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("match", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/regex", strings.NewReader(`{"pattern":"[a-z]+","text":"Hello World"}`))
		w := httptest.NewRecorder()
		apiTextRegexHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		matches, ok := data["matches"].([]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, matches)
	})

	t.Run("replace", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/regex", strings.NewReader(`{"pattern":"o","text":"foo","mode":"replace","replacement":"0"}`))
		w := httptest.NewRecorder()
		apiTextRegexHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "f00", data["result"])
	})

	t.Run("explain", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/regex", strings.NewReader(`{"pattern":"[a-z]+","mode":"explain"}`))
		w := httptest.NewRecorder()
		apiTextRegexHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["explanation"])
	})

	t.Run("invalid pattern", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/regex", strings.NewReader(`{"pattern":"[","text":"x"}`))
		w := httptest.NewRecorder()
		apiTextRegexHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_PATTERN", env["error"])
	})

	t.Run("invalid mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/regex", strings.NewReader(`{"pattern":"x","mode":"bogus"}`))
		w := httptest.NewRecorder()
		apiTextRegexHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})
}

func TestAPICryptoEncryptDecryptHandlers(t *testing.T) {
	t.Run("encrypt missing plaintext", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/encrypt", strings.NewReader(`{"key":"secret"}`))
		w := httptest.NewRecorder()
		apiCryptoEncryptHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("encrypt missing key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/encrypt", strings.NewReader(`{"plaintext":"hello"}`))
		w := httptest.NewRecorder()
		apiCryptoEncryptHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("decrypt missing ciphertext", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt", strings.NewReader(`{"key":"secret"}`))
		w := httptest.NewRecorder()
		apiCryptoDecryptHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("decrypt missing key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt", strings.NewReader(`{"ciphertext":"abc"}`))
		w := httptest.NewRecorder()
		apiCryptoDecryptHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("decrypt with wrong key fails", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt", strings.NewReader(`{"ciphertext":"bm90LXJlYWwtY2lwaGVydGV4dA==","key":"wrong"}`))
		w := httptest.NewRecorder()
		apiCryptoDecryptHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "DECRYPT_FAILED", env["error"])
	})

	t.Run("round trip", func(t *testing.T) {
		encReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/encrypt", strings.NewReader(`{"plaintext":"Hello World","key":"correct horse battery staple"}`))
		encW := httptest.NewRecorder()
		apiCryptoEncryptHandler(encW, encReq)
		assert.Equal(t, http.StatusOK, encW.Code)
		encEnv := decodeEnvelope(t, encW.Body.Bytes())
		encData, ok := encEnv["data"].(map[string]interface{})
		require.True(t, ok)
		ciphertext, ok := encData["ciphertext"].(string)
		require.True(t, ok)
		require.NotEmpty(t, ciphertext)

		decReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt", strings.NewReader(`{"ciphertext":"`+ciphertext+`","key":"correct horse battery staple"}`))
		decW := httptest.NewRecorder()
		apiCryptoDecryptHandler(decW, decReq)
		assert.Equal(t, http.StatusOK, decW.Code)
		decEnv := decodeEnvelope(t, decW.Body.Bytes())
		decData, ok := decEnv["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "Hello World", decData["plaintext"])
	})
}

func TestAPICryptoRSAHandler(t *testing.T) {
	t.Run("generate default bits", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(`{"mode":"generate"}`))
		w := httptest.NewRecorder()
		apiCryptoRSAHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["private_key"])
		assert.NotEmpty(t, data["public_key"])
	})

	t.Run("encrypt missing plaintext", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(`{"mode":"encrypt","public_key":"x"}`))
		w := httptest.NewRecorder()
		apiCryptoRSAHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("encrypt missing public key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(`{"mode":"encrypt","plaintext":"hi"}`))
		w := httptest.NewRecorder()
		apiCryptoRSAHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("decrypt missing ciphertext", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(`{"mode":"decrypt","private_key":"x"}`))
		w := httptest.NewRecorder()
		apiCryptoRSAHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("decrypt missing private key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(`{"mode":"decrypt","ciphertext":"x"}`))
		w := httptest.NewRecorder()
		apiCryptoRSAHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("invalid mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(`{"mode":"bogus"}`))
		w := httptest.NewRecorder()
		apiCryptoRSAHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("round trip", func(t *testing.T) {
		genReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(`{"mode":"generate","bits":2048}`))
		genW := httptest.NewRecorder()
		apiCryptoRSAHandler(genW, genReq)
		require.Equal(t, http.StatusOK, genW.Code)
		genEnv := decodeEnvelope(t, genW.Body.Bytes())
		genData, ok := genEnv["data"].(map[string]interface{})
		require.True(t, ok)
		privateKey, ok := genData["private_key"].(string)
		require.True(t, ok)
		publicKey, ok := genData["public_key"].(string)
		require.True(t, ok)

		encBody, err := json.Marshal(map[string]string{
			"mode":       "encrypt",
			"plaintext":  "Hello World",
			"public_key": publicKey,
		})
		require.NoError(t, err)
		encReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(string(encBody)))
		encW := httptest.NewRecorder()
		apiCryptoRSAHandler(encW, encReq)
		require.Equal(t, http.StatusOK, encW.Code)
		encEnv := decodeEnvelope(t, encW.Body.Bytes())
		encData, ok := encEnv["data"].(map[string]interface{})
		require.True(t, ok)
		ciphertext, ok := encData["ciphertext"].(string)
		require.True(t, ok)
		require.NotEmpty(t, ciphertext)

		decBody, err := json.Marshal(map[string]string{
			"mode":        "decrypt",
			"ciphertext":  ciphertext,
			"private_key": privateKey,
		})
		require.NoError(t, err)
		decReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(string(decBody)))
		decW := httptest.NewRecorder()
		apiCryptoRSAHandler(decW, decReq)
		require.Equal(t, http.StatusOK, decW.Code)
		decEnv := decodeEnvelope(t, decW.Body.Bytes())
		decData, ok := decEnv["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "Hello World", decData["plaintext"])
	})
}

func TestAPICryptoHMACHandler(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/hmac", strings.NewReader(`{"message":"hi"}`))
		w := httptest.NewRecorder()
		apiCryptoHMACHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("default algorithm sha256", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/hmac", strings.NewReader(`{"key":"secret","message":"Hello World"}`))
		w := httptest.NewRecorder()
		apiCryptoHMACHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "sha256", data["algorithm"])
		assert.NotEmpty(t, data["hmac"])
	})

	t.Run("explicit sha1", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/hmac", strings.NewReader(`{"algorithm":"sha1","key":"secret","message":"Hello World"}`))
		w := httptest.NewRecorder()
		apiCryptoHMACHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "sha1", data["algorithm"])
		assert.NotEmpty(t, data["hmac"])
	})

	t.Run("invalid algorithm", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/hmac", strings.NewReader(`{"algorithm":"md5","key":"secret","message":"hi"}`))
		w := httptest.NewRecorder()
		apiCryptoHMACHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_ALGORITHM", env["error"])
	})
}

func TestAPICryptoCertificateHandler(t *testing.T) {
	t.Run("generate missing common name", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/certificate", strings.NewReader(`{"mode":"generate"}`))
		w := httptest.NewRecorder()
		apiCryptoCertificateHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("invalid mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/certificate", strings.NewReader(`{"mode":"bogus"}`))
		w := httptest.NewRecorder()
		apiCryptoCertificateHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("generate then parse round trip", func(t *testing.T) {
		genReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/certificate", strings.NewReader(`{"mode":"generate","common_name":"example.com","valid_days":30}`))
		genW := httptest.NewRecorder()
		apiCryptoCertificateHandler(genW, genReq)
		require.Equal(t, http.StatusOK, genW.Code)
		genEnv := decodeEnvelope(t, genW.Body.Bytes())
		genData, ok := genEnv["data"].(map[string]interface{})
		require.True(t, ok)
		certPEM, ok := genData["certificate"].(string)
		require.True(t, ok)
		require.NotEmpty(t, certPEM)

		parseBody, err := json.Marshal(map[string]string{
			"mode":        "parse",
			"certificate": certPEM,
		})
		require.NoError(t, err)
		parseReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/certificate", strings.NewReader(string(parseBody)))
		parseW := httptest.NewRecorder()
		apiCryptoCertificateHandler(parseW, parseReq)
		require.Equal(t, http.StatusOK, parseW.Code)
		parseEnv := decodeEnvelope(t, parseW.Body.Bytes())
		parseData, ok := parseEnv["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, parseData["subject"], "example.com")
	})

	t.Run("parse invalid pem", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/certificate", strings.NewReader(`{"mode":"parse","certificate":"not a cert"}`))
		w := httptest.NewRecorder()
		apiCryptoCertificateHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "PARSE_FAILED", env["error"])
	})
}

func TestAPICryptoEd25519Handler(t *testing.T) {
	t.Run("generate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/ed25519", strings.NewReader(`{"mode":"generate"}`))
		w := httptest.NewRecorder()
		apiCryptoEd25519Handler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["private_key"])
		assert.NotEmpty(t, data["public_key"])
	})

	t.Run("invalid mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/ed25519", strings.NewReader(`{"mode":"bogus"}`))
		w := httptest.NewRecorder()
		apiCryptoEd25519Handler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("sign then verify round trip", func(t *testing.T) {
		genReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/ed25519", strings.NewReader(`{"mode":"generate"}`))
		genW := httptest.NewRecorder()
		apiCryptoEd25519Handler(genW, genReq)
		require.Equal(t, http.StatusOK, genW.Code)
		genEnv := decodeEnvelope(t, genW.Body.Bytes())
		genData, ok := genEnv["data"].(map[string]interface{})
		require.True(t, ok)
		privateKey, ok := genData["private_key"].(string)
		require.True(t, ok)
		publicKey, ok := genData["public_key"].(string)
		require.True(t, ok)

		signBody, err := json.Marshal(map[string]string{
			"mode":        "sign",
			"message":     "Hello World",
			"private_key": privateKey,
		})
		require.NoError(t, err)
		signReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/ed25519", strings.NewReader(string(signBody)))
		signW := httptest.NewRecorder()
		apiCryptoEd25519Handler(signW, signReq)
		require.Equal(t, http.StatusOK, signW.Code)
		signEnv := decodeEnvelope(t, signW.Body.Bytes())
		signData, ok := signEnv["data"].(map[string]interface{})
		require.True(t, ok)
		signature, ok := signData["signature"].(string)
		require.True(t, ok)
		require.NotEmpty(t, signature)

		verifyBody, err := json.Marshal(map[string]string{
			"mode":       "verify",
			"message":    "Hello World",
			"signature":  signature,
			"public_key": publicKey,
		})
		require.NoError(t, err)
		verifyReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/ed25519", strings.NewReader(string(verifyBody)))
		verifyW := httptest.NewRecorder()
		apiCryptoEd25519Handler(verifyW, verifyReq)
		require.Equal(t, http.StatusOK, verifyW.Code)
		verifyEnv := decodeEnvelope(t, verifyW.Body.Bytes())
		verifyData, ok := verifyEnv["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, verifyData["valid"])
	})
}

func TestAPICryptoPGPHandler(t *testing.T) {
	t.Run("generate missing identity", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/pgp", strings.NewReader(`{"mode":"generate"}`))
		w := httptest.NewRecorder()
		apiCryptoPGPHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("invalid mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/pgp", strings.NewReader(`{"mode":"bogus"}`))
		w := httptest.NewRecorder()
		apiCryptoPGPHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("generate encrypt decrypt round trip", func(t *testing.T) {
		genReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/pgp", strings.NewReader(`{"mode":"generate","name":"Jane Doe","email":"jane@example.com"}`))
		genW := httptest.NewRecorder()
		apiCryptoPGPHandler(genW, genReq)
		require.Equal(t, http.StatusOK, genW.Code)
		genEnv := decodeEnvelope(t, genW.Body.Bytes())
		genData, ok := genEnv["data"].(map[string]interface{})
		require.True(t, ok)
		publicKey, ok := genData["public_key"].(string)
		require.True(t, ok)
		privateKey, ok := genData["private_key"].(string)
		require.True(t, ok)
		require.NotEmpty(t, publicKey)
		require.NotEmpty(t, privateKey)

		encBody, err := json.Marshal(map[string]string{
			"mode":       "encrypt",
			"plaintext":  "Hello World",
			"public_key": publicKey,
		})
		require.NoError(t, err)
		encReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/pgp", strings.NewReader(string(encBody)))
		encW := httptest.NewRecorder()
		apiCryptoPGPHandler(encW, encReq)
		require.Equal(t, http.StatusOK, encW.Code)
		encEnv := decodeEnvelope(t, encW.Body.Bytes())
		encData, ok := encEnv["data"].(map[string]interface{})
		require.True(t, ok)
		ciphertext, ok := encData["ciphertext"].(string)
		require.True(t, ok)
		require.NotEmpty(t, ciphertext)

		decBody, err := json.Marshal(map[string]string{
			"mode":        "decrypt",
			"ciphertext":  ciphertext,
			"private_key": privateKey,
		})
		require.NoError(t, err)
		decReq := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/pgp", strings.NewReader(string(decBody)))
		decW := httptest.NewRecorder()
		apiCryptoPGPHandler(decW, decReq)
		require.Equal(t, http.StatusOK, decW.Code)
		decEnv := decodeEnvelope(t, decW.Body.Bytes())
		decData, ok := decEnv["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "Hello World", decData["plaintext"])
	})
}

// apiDockerLintHandler must 400 MISSING_DOCKERFILE for an empty body and 200
// with a lint result for valid Dockerfile content.
func TestAPIDockerLintHandler(t *testing.T) {
	t.Run("missing dockerfile", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/dockerfile-lint", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiDockerLintHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid dockerfile", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/dockerfile-lint", strings.NewReader("FROM ubuntu:latest\nRUN apt-get update"))
		w := httptest.NewRecorder()
		apiDockerLintHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
	})
}

// apiDockerBestPracticesHandler must always return 200 with a non-empty
// tips list.
func TestAPIDockerBestPracticesHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/docker/best-practices", nil)
	w := httptest.NewRecorder()
	apiDockerBestPracticesHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	tips, ok := data["tips"].([]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, tips)
}

// apiDockerComposeValidateHandler must 400 MISSING_COMPOSE for an empty body
// and 200 with a validation result for valid compose YAML.
func TestAPIDockerComposeValidateHandler(t *testing.T) {
	t.Run("missing compose", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/compose-validate", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiDockerComposeValidateHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid compose", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/compose-validate", strings.NewReader("services:\n  web:\n    image: nginx:latest\n"))
		w := httptest.NewRecorder()
		apiDockerComposeValidateHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, data["valid"])
	})
}

// apiDockerComposeToRunHandler must 400 MISSING_COMPOSE for an empty body,
// 400 INVALID_COMPOSE for unparseable YAML, and 200 with a generated docker
// run command for valid single-service compose YAML.
func TestAPIDockerComposeToRunHandler(t *testing.T) {
	t.Run("missing compose", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/compose-to-run", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiDockerComposeToRunHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("invalid compose", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/compose-to-run", strings.NewReader("not: [valid"))
		w := httptest.NewRecorder()
		apiDockerComposeToRunHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_COMPOSE", env["error"])
	})

	t.Run("valid compose", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/compose-to-run?service=web", strings.NewReader("services:\n  web:\n    image: nginx:latest\n"))
		w := httptest.NewRecorder()
		apiDockerComposeToRunHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["command"], "nginx:latest")
	})
}

// apiDockerRunToComposeHandler must 400 MISSING_COMMAND for an empty body
// and 200 with a generated compose block for a valid docker run command.
func TestAPIDockerRunToComposeHandler(t *testing.T) {
	t.Run("missing command", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/run-to-compose", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiDockerRunToComposeHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid command", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/run-to-compose", strings.NewReader("docker run -d --name web -p 8080:80 nginx:latest"))
		w := httptest.NewRecorder()
		apiDockerRunToComposeHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["compose"], "nginx:latest")
	})
}

// apiDockerEnvParserHandler must 400 MISSING_ENV for an empty body and 200
// with parsed variables for a valid .env body.
func TestAPIDockerEnvParserHandler(t *testing.T) {
	t.Run("missing env", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/env-parser", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiDockerEnvParserHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid env", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/env-parser", strings.NewReader("PORT=8080\nDEBUG=true\n"))
		w := httptest.NewRecorder()
		apiDockerEnvParserHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		vars, ok := data["variables"].([]interface{})
		require.True(t, ok)
		require.Len(t, vars, 2)
	})
}

// apiDockerNetworkHelperHandler must 400 INVALID_NETWORK_CONFIG for a
// missing name and 200 with a generated command/compose block otherwise.
func TestAPIDockerNetworkHelperHandler(t *testing.T) {
	t.Run("missing name", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/docker/network-helper", nil)
		w := httptest.NewRecorder()
		apiDockerNetworkHelperHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_NETWORK_CONFIG", env["error"])
	})

	t.Run("valid config", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/docker/network-helper?name=app-net&driver=bridge", nil)
		w := httptest.NewRecorder()
		apiDockerNetworkHelperHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["run_command"], "app-net")
	})
}

// apiDockerSecurityScanHandler must 400 MISSING_CONTENT for an empty body
// and 200 with a scan result for valid Dockerfile content.
func TestAPIDockerSecurityScanHandler(t *testing.T) {
	t.Run("missing content", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/security-scan", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiDockerSecurityScanHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid content", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/security-scan", strings.NewReader("FROM ubuntu:latest\nUSER root\n"))
		w := httptest.NewRecorder()
		apiDockerSecurityScanHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
	})
}

// apiDockerSizeOptimizerHandler must 400 MISSING_DOCKERFILE for an empty
// body and 200 with suggestions for valid Dockerfile content.
func TestAPIDockerSizeOptimizerHandler(t *testing.T) {
	t.Run("missing dockerfile", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/size-optimizer", strings.NewReader(""))
		w := httptest.NewRecorder()
		apiDockerSizeOptimizerHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid dockerfile", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/size-optimizer", strings.NewReader("FROM golang:latest\nRUN apt-get update && apt-get install -y curl\n"))
		w := httptest.NewRecorder()
		apiDockerSizeOptimizerHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		suggestions, ok := data["suggestions"].([]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, suggestions)
	})
}

func TestAPIGenerateBarcodeHandler(t *testing.T) {
	t.Run("missing data", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/barcode?format=code128", nil)
		w := httptest.NewRecorder()
		apiGenerateBarcodeHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid code128", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/barcode?format=code128&data=Hello123", nil)
		w := httptest.NewRecorder()
		apiGenerateBarcodeHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		_, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		require.NoError(t, err)
	})

	t.Run("unsupported format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/barcode?format=nope&data=abc", nil)
		w := httptest.NewRecorder()
		apiGenerateBarcodeHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "BARCODE_GENERATION_FAILED", env["error"])
	})
}

func TestAPIGenerateAvatarHandler(t *testing.T) {
	t.Run("missing initials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/avatar", nil)
		w := httptest.NewRecorder()
		apiGenerateAvatarHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid initials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/avatar?initials=AB&size=64", nil)
		w := httptest.NewRecorder()
		apiGenerateAvatarHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		img, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		require.NoError(t, err)
		assert.Equal(t, 64, img.Bounds().Dx())
	})
}

func TestAPIGenerateIdenticonHandler(t *testing.T) {
	t.Run("missing seed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/identicon", nil)
		w := httptest.NewRecorder()
		apiGenerateIdenticonHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid seed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/identicon?seed=user@example.com", nil)
		w := httptest.NewRecorder()
		apiGenerateIdenticonHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		_, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		require.NoError(t, err)
	})
}

func TestAPIGenerateDockerfileHandler(t *testing.T) {
	t.Run("default generic", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/dockerfile", nil)
		w := httptest.NewRecorder()
		apiGenerateDockerfileHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["dockerfile"], "FROM alpine")
	})

	t.Run("unsupported lang", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/dockerfile?lang=cobol", nil)
		w := httptest.NewRecorder()
		apiGenerateDockerfileHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "DOCKERFILE_GENERATION_FAILED", env["error"])
	})
}

func TestAPIGenerateGitignoreHandler(t *testing.T) {
	t.Run("valid langs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/gitignore?lang=go,vscode", nil)
		w := httptest.NewRecorder()
		apiGenerateGitignoreHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["gitignore"], "# Go")
	})

	t.Run("missing lang", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/gitignore", nil)
		w := httptest.NewRecorder()
		apiGenerateGitignoreHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "GITIGNORE_GENERATION_FAILED", env["error"])
	})
}

func TestAPIGenerateLicenseHandler(t *testing.T) {
	t.Run("mit with author/year", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/license?type=mit&author=Jane+Doe&year=2026", nil)
		w := httptest.NewRecorder()
		apiGenerateLicenseHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["license"], "Copyright (c) 2026 Jane Doe")
	})

	t.Run("unsupported type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/license?type=wtfpl", nil)
		w := httptest.NewRecorder()
		apiGenerateLicenseHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "LICENSE_GENERATION_FAILED", env["error"])
	})
}

func TestAPIGenerateConfigHandler(t *testing.T) {
	t.Run("yaml with values", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/config?format=yaml&host=localhost&port=8080", nil)
		w := httptest.NewRecorder()
		apiGenerateConfigHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["config"], "host: localhost")
	})

	t.Run("no values", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/config?format=yaml", nil)
		w := httptest.NewRecorder()
		apiGenerateConfigHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "CONFIG_GENERATION_FAILED", env["error"])
	})
}

func TestAPIGenerateSQLHandler(t *testing.T) {
	t.Run("valid table", func(t *testing.T) {
		body := `{"table":"users","columns":[{"name":"id","type":"integer","primary_key":true}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/generate/sql", strings.NewReader(body))
		w := httptest.NewRecorder()
		apiGenerateSQLHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["sql"], "CREATE TABLE users (")
	})

	t.Run("missing table", func(t *testing.T) {
		body := `{"columns":[{"name":"id","type":"integer"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/generate/sql", strings.NewReader(body))
		w := httptest.NewRecorder()
		apiGenerateSQLHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "SQL_GENERATION_FAILED", env["error"])
	})

	t.Run("invalid body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/generate/sql", strings.NewReader("not json"))
		w := httptest.NewRecorder()
		apiGenerateSQLHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_BODY", env["error"])
	})
}

func TestAPIGenerateSSHKeyHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/ssh-key", nil)
	w := httptest.NewRecorder()
	apiGenerateSSHKeyHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, data["private_key"], "BEGIN OPENSSH PRIVATE KEY")
	assert.Contains(t, data["public_key"], "ssh-ed25519")
}

func TestAPIGenerateAPIDocsHandler(t *testing.T) {
	t.Run("default markdown", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/api-docs", nil)
		w := httptest.NewRecorder()
		apiGenerateAPIDocsHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["docs"], "Version: v1")
	})

	t.Run("json format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/generate/api-docs?format=json", nil)
		w := httptest.NewRecorder()
		apiGenerateAPIDocsHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, data["docs"], "paths")
	})
}

func TestAPIGeneratePlaceholderHandler(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/generate/placeholder/{width}/{height}", apiGeneratePlaceholderHandler)

	t.Run("valid dimensions", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/generate/placeholder/100/50", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		img, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		require.NoError(t, err)
		assert.Equal(t, 100, img.Bounds().Dx())
		assert.Equal(t, 50, img.Bounds().Dy())
	})

	t.Run("invalid width", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/generate/placeholder/notanumber/50", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_WIDTH", env["error"])
	})
}

// apiGeoCountryHandler must resolve a known country and reject a missing or
// unknown query. Network-independent: no live geocoding/timezone calls are
// exercised at the handler layer here (covered by the geo package's own
// mocked service-layer tests instead).
func TestAPIGeoCountryHandler(t *testing.T) {
	t.Run("missing query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/country", nil)
		w := httptest.NewRecorder()
		apiGeoCountryHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("valid country", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/country?q=US", nil)
		w := httptest.NewRecorder()
		apiGeoCountryHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "US", data["alpha2"])
	})

	t.Run("unknown country", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/country?q=NotACountry", nil)
		w := httptest.NewRecorder()
		apiGeoCountryHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "COUNTRY_NOT_FOUND", env["error"])
	})
}

// apiGeoGeohashHandler must encode and decode correctly, and reject invalid
// input for both modes.
func TestAPIGeoGeohashHandler(t *testing.T) {
	t.Run("encode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/geohash?lat=40.7128&lon=-74.0060&precision=9", nil)
		w := httptest.NewRecorder()
		apiGeoGeohashHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["geohash"])
	})

	t.Run("decode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/geohash?hash=dr5regw3p", nil)
		w := httptest.NewRecorder()
		apiGeoGeohashHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "dr5regw3p", data["geohash"])
	})

	t.Run("invalid coordinates", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/geohash", nil)
		w := httptest.NewRecorder()
		apiGeoGeohashHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_COORDINATES", env["error"])
	})
}

// apiGeoH3Handler must encode a valid coordinate and reject a missing
// coordinate.
func TestAPIGeoH3Handler(t *testing.T) {
	t.Run("valid encode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/h3?lat=40.7128&lon=-74.0060&resolution=9", nil)
		w := httptest.NewRecorder()
		apiGeoH3Handler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["index"])
	})

	t.Run("invalid coordinates", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/h3", nil)
		w := httptest.NewRecorder()
		apiGeoH3Handler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_COORDINATES", env["error"])
	})
}

// apiGeoPlusCodeHandler must encode a valid coordinate and decode a valid
// plus code.
func TestAPIGeoPlusCodeHandler(t *testing.T) {
	t.Run("encode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/pluscode?lat=40.7128&lon=-74.0060", nil)
		w := httptest.NewRecorder()
		apiGeoPlusCodeHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, data["code"])
	})

	t.Run("decode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/pluscode?code=87G7PX7V%2B3F", nil)
		w := httptest.NewRecorder()
		apiGeoPlusCodeHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "87G7PX7V+3F", data["code"])
	})

	t.Run("invalid code", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/pluscode?code=not-a-code", nil)
		w := httptest.NewRecorder()
		apiGeoPlusCodeHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_PLUS_CODE", env["error"])
	})
}

// apiGeoBBoxHandler must compute a box from center+radius, compute a box
// from a coordinate list, and reject missing/invalid input for both modes.
func TestAPIGeoBBoxHandler(t *testing.T) {
	t.Run("radius mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/bbox?lat=40.7128&lon=-74.0060&radius=10", nil)
		w := httptest.NewRecorder()
		apiGeoBBoxHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["min_latitude"])
	})

	t.Run("radius required", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/bbox?lat=40.7128&lon=-74.0060", nil)
		w := httptest.NewRecorder()
		apiGeoBBoxHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "VALIDATION_FAILED", env["error"])
	})

	t.Run("coords list mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/bbox?coords=40.7128,-74.0060|34.0522,-118.2437", nil)
		w := httptest.NewRecorder()
		apiGeoBBoxHandler(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, data["max_latitude"])
	})

	t.Run("invalid coords entry", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/bbox?coords=notacoord", nil)
		w := httptest.NewRecorder()
		apiGeoBBoxHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_COORDS", env["error"])
	})
}
