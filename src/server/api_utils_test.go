package server

import (
	"bytes"
	"encoding/json"
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
		assert.Equal(t, "INVALID_COUNT", env["error"])
	})
}

func TestAPIMathBaseHandler(t *testing.T) {
	t.Run("missing params", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/base", nil)
		w := httptest.NewRecorder()

		apiMathBaseHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_PARAMS", env["error"])
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
		assert.Equal(t, "MISSING_MATRIX", env["error"])
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
		assert.Equal(t, "INVALID_OPERATION", env["error"])
	})
}

func TestAPIMathSequenceHandler(t *testing.T) {
	t.Run("missing params", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/math/sequence", nil)
		w := httptest.NewRecorder()

		apiMathSequenceHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_PARAMS", env["error"])
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
		assert.Equal(t, "MISSING_VALUE", env["error"])
	})

	t.Run("missing format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/convert/color?value=%23ff0000", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_FORMAT", env["error"])
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
		assert.Equal(t, "MISSING_CURRENCY", env["error"])
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

func TestAPIValidateCreditCardHandler(t *testing.T) {
	t.Run("missing number", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/validate/credit-card", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiValidateCreditCardHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_NUMBER", env["error"])
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
		assert.Equal(t, "MISSING_DOMAIN", env["error"])
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
		assert.Equal(t, "MISSING_IP", env["error"])
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
		assert.Equal(t, "MISSING_MAC", env["error"])
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
		assert.Equal(t, "MISSING_PHONE", env["error"])
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
		assert.Equal(t, "MISSING_URL", env["error"])
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
		assert.Equal(t, "MISSING_UUID", env["error"])
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
		assert.Equal(t, "MISSING_IBAN", env["error"])
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
		assert.Equal(t, "MISSING_ISBN", env["error"])
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
		assert.Equal(t, "MISSING_VAT", env["error"])
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
		assert.Equal(t, "MISSING_XML", env["error"])
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
		assert.Equal(t, "MISSING_CSV", env["error"])
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
		assert.Equal(t, "MISSING_FIELDS", env["error"])
	})

	t.Run("invalid op", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test/assert", strings.NewReader(`{"op":"bogus"}`))
		w := httptest.NewRecorder()
		apiTestAssertHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_OP", env["error"])
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
		assert.Equal(t, "INVALID_TYPE", env["error"])
	})
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
	assert.Equal(t, "MISSING_DOMAIN", env["error"])
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
	assert.Equal(t, "MISSING_DOMAIN", env["error"])
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
		assert.Equal(t, "MISSING_FIELDS", env["error"])
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

// apiTextCompressHandler must 400 MISSING_DATA when data is absent, default
// to gzip compress mode, and round-trip through decompress.
func TestAPITextCompressHandler(t *testing.T) {
	t.Run("missing data", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/text/compress", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		apiTextCompressHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_DATA", env["error"])
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
		assert.Equal(t, "INVALID_MODE", env["error"])
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
		assert.Equal(t, "INVALID_TYPE", env["error"])
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
		assert.Equal(t, "MISSING_PATTERN", env["error"])
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
		assert.Equal(t, "INVALID_MODE", env["error"])
	})
}

func TestAPICryptoEncryptDecryptHandlers(t *testing.T) {
	t.Run("encrypt missing plaintext", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/encrypt", strings.NewReader(`{"key":"secret"}`))
		w := httptest.NewRecorder()
		apiCryptoEncryptHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_PLAINTEXT", env["error"])
	})

	t.Run("encrypt missing key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/encrypt", strings.NewReader(`{"plaintext":"hello"}`))
		w := httptest.NewRecorder()
		apiCryptoEncryptHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_KEY", env["error"])
	})

	t.Run("decrypt missing ciphertext", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt", strings.NewReader(`{"key":"secret"}`))
		w := httptest.NewRecorder()
		apiCryptoDecryptHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_CIPHERTEXT", env["error"])
	})

	t.Run("decrypt missing key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt", strings.NewReader(`{"ciphertext":"abc"}`))
		w := httptest.NewRecorder()
		apiCryptoDecryptHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_KEY", env["error"])
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
		assert.Equal(t, "MISSING_PLAINTEXT", env["error"])
	})

	t.Run("encrypt missing public key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(`{"mode":"encrypt","plaintext":"hi"}`))
		w := httptest.NewRecorder()
		apiCryptoRSAHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_PUBLIC_KEY", env["error"])
	})

	t.Run("decrypt missing ciphertext", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(`{"mode":"decrypt","private_key":"x"}`))
		w := httptest.NewRecorder()
		apiCryptoRSAHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_CIPHERTEXT", env["error"])
	})

	t.Run("decrypt missing private key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(`{"mode":"decrypt","ciphertext":"x"}`))
		w := httptest.NewRecorder()
		apiCryptoRSAHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_PRIVATE_KEY", env["error"])
	})

	t.Run("invalid mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rsa", strings.NewReader(`{"mode":"bogus"}`))
		w := httptest.NewRecorder()
		apiCryptoRSAHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_MODE", env["error"])
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
		assert.Equal(t, "MISSING_KEY", env["error"])
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
