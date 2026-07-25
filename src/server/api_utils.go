package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/service/convert"
	"github.com/apimgr/api/src/service/crypto"
	"github.com/apimgr/api/src/service/dev"
	"github.com/apimgr/api/src/service/docker"
	"github.com/apimgr/api/src/service/fun"
	"github.com/apimgr/api/src/service/geo"
	"github.com/apimgr/api/src/service/image"
	"github.com/apimgr/api/src/service/language"
	"github.com/apimgr/api/src/service/lorem"
	"github.com/apimgr/api/src/service/math"
	"github.com/apimgr/api/src/service/osint"
	"github.com/apimgr/api/src/service/parse"
	"github.com/apimgr/api/src/service/research"
	"github.com/apimgr/api/src/service/test"
	"github.com/apimgr/api/src/service/text"
	"github.com/apimgr/api/src/service/validate"
	"github.com/apimgr/api/src/service/weather"
	"github.com/go-chi/chi/v5"
)

// Shared service singletons backing the handlers in this file.
var (
	dockerService   = docker.New()
	weatherService  = weather.New()
	mathService     = math.New()
	convertService  = convert.New()
	validateService = validate.New()
	parseService    = parse.New()
	testService     = test.New()
	osintService    = osint.New()
	funService      = fun.New()
	geoService      = geo.New()
	loremService    = lorem.New()
	devService      = dev.New()
	imageService    = image.New()
	languageService = language.New()
	researchService = research.New()
)

// readRequestBody reads and returns the entire request body, capped at
// 1MB to bound memory use for handlers that parse the raw body text.
func readRequestBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

// decodeJSONBody decodes the request body as JSON into dst.
func decodeJSONBody(r *http.Request, dst interface{}) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dst)
}

// queryOrJSONField reads a string field first from the ?{queryKey}= query
// parameter, falling back to the same-named field in a JSON request body.
// Shared by the single-value validate/* handlers so each one only needs to
// know its own field name and validator function.
func queryOrJSONField(r *http.Request, queryKey string) string {
	if v := r.URL.Query().Get(queryKey); v != "" {
		return v
	}
	var body map[string]string
	if err := decodeJSONBody(r, &body); err == nil {
		return body[queryKey]
	}
	return ""
}

// apiDockerVersionHandler parses a docker image reference passed as
// ?image= and reports its registry/namespace/repository/tag breakdown.
// docker.Service has no daemon-version concept; the closest available
// "version" is the parsed image tag.
func apiDockerVersionHandler(w http.ResponseWriter, r *http.Request) {
	image := r.URL.Query().Get("image")
	if image == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_IMAGE", "image query parameter is required", nil)
		return
	}
	info := dockerService.ParseImageName(image)
	writeEnvelopeOK(w, http.StatusOK, info)
}

// apiDockerPortMappingHandler formats a host/container/protocol triple into
// a "host:container/protocol" string (?action=format, the default) or
// parses an existing mapping string back into its parts
// (?action=parse&mapping=), using docker.Service's Format/ParsePortMapping.
func apiDockerPortMappingHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	action := q.Get("action")
	if action == "" {
		action = "format"
	}

	switch action {
	case "format":
		hostPort, err := strconv.Atoi(q.Get("host"))
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_HOST_PORT", "host query parameter must be an integer", nil)
			return
		}
		containerPort, err := strconv.Atoi(q.Get("container"))
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_CONTAINER_PORT", "container query parameter must be an integer", nil)
			return
		}
		mapping := dockerService.FormatPortMapping(hostPort, containerPort, q.Get("protocol"))
		writeEnvelopeOK(w, http.StatusOK, map[string]string{"mapping": mapping})
	case "parse":
		mapping := q.Get("mapping")
		if mapping == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_MAPPING", "mapping query parameter is required", nil)
			return
		}
		hostPort, containerPort, protocol, err := dockerService.ParsePortMapping(mapping)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MAPPING", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"host_port":      hostPort,
			"container_port": containerPort,
			"protocol":       protocol,
		})
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_ACTION", "action must be format or parse", nil)
	}
}

// apiDockerVolumeHandler formats a host/container path pair into a
// "host:container[:ro]" volume mount string, using
// docker.Service.FormatVolumeMount.
func apiDockerVolumeHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	hostPath := q.Get("host")
	containerPath := q.Get("container")
	if hostPath == "" || containerPath == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_PATH", "host and container query parameters are required", nil)
		return
	}
	readOnly := config.IsTruthy(q.Get("readonly"))
	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"mount": dockerService.FormatVolumeMount(hostPath, containerPath, readOnly),
	})
}

// apiDockerfileGenerateHandler decodes a JSON docker.DockerfileConfig from
// the request body and returns the generated Dockerfile text, using
// docker.Service.GenerateDockerfile.
func apiDockerfileGenerateHandler(w http.ResponseWriter, r *http.Request) {
	var cfg docker.DockerfileConfig
	if err := decodeJSONBody(r, &cfg); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if cfg.BaseImage == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_BASE_IMAGE", "base_image is required", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"dockerfile": dockerService.GenerateDockerfile(cfg),
	})
}

// apiWeatherCurrentHandler returns current weather for the {location}
// path parameter.
func apiWeatherCurrentHandler(w http.ResponseWriter, r *http.Request) {
	location := chi.URLParam(r, "location")
	weatherData, err := weatherService.GetCurrentWeather(location)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "WEATHER_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, weatherData)
}

// apiWeatherForecastHandler returns a daily weather forecast for the
// {location} path parameter. The number of days (1-16) is read from the
// ?days= query parameter, defaulting to 5.
func apiWeatherForecastHandler(w http.ResponseWriter, r *http.Request) {
	location := chi.URLParam(r, "location")
	days := 5
	if raw := r.URL.Query().Get("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_DAYS", "days must be an integer between 1 and 16", nil)
			return
		}
		days = parsed
	}
	forecast, err := weatherService.GetForecast(location, days)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "WEATHER_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"location": location,
		"days":     days,
		"forecast": forecast,
	})
}

// apiGeoIPHandler resolves geolocation for the {ip} path parameter. The
// geo service package only implements coordinate math (distance, bearing,
// midpoint) with no IP capability, so this deliberately reuses
// osint.IPLookup, which already implements the required MaxMind-backed
// lookup plus private/loopback/link-local rejection.
func apiGeoIPHandler(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	info, err := osintService.IPLookup(ip)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IP_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, info)
}

// parseGeoCoordinateParams parses the lat1/lon1/lat2/lon2 query parameters
// shared by the two-point geo.Service operations (distance, bearing,
// midpoint), returning a descriptive error if any are missing or invalid.
func parseGeoCoordinateParams(q url.Values) (lat1, lon1, lat2, lon2 float64, err error) {
	parse := func(name string) (float64, error) {
		raw := q.Get(name)
		if raw == "" {
			return 0, fmt.Errorf("%s query parameter is required", name)
		}
		v, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("%s must be a number", name)
		}
		return v, nil
	}

	if lat1, err = parse("lat1"); err != nil {
		return
	}
	if lon1, err = parse("lon1"); err != nil {
		return
	}
	if lat2, err = parse("lat2"); err != nil {
		return
	}
	if lon2, err = parse("lon2"); err != nil {
		return
	}
	return
}

// apiGeoDistanceHandler computes the great-circle distance between two
// coordinates, in kilometers (default) or miles via ?unit=mi.
func apiGeoDistanceHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat1, lon1, lat2, lon2, err := parseGeoCoordinateParams(q)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COORDINATES", err.Error(), nil)
		return
	}

	unit := strings.ToLower(q.Get("unit"))
	if unit == "" {
		unit = "km"
	}

	var distance float64
	switch unit {
	case "km":
		distance = geoService.Distance(lat1, lon1, lat2, lon2)
	case "mi":
		distance = geoService.DistanceInMiles(lat1, lon1, lat2, lon2)
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_UNIT", "unit must be km or mi", nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"distance": distance,
		"unit":     unit,
	})
}

// apiGeoBearingHandler computes the initial compass bearing (in degrees)
// from one coordinate to another.
func apiGeoBearingHandler(w http.ResponseWriter, r *http.Request) {
	lat1, lon1, lat2, lon2, err := parseGeoCoordinateParams(r.URL.Query())
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COORDINATES", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]float64{
		"bearing": geoService.Bearing(lat1, lon1, lat2, lon2),
	})
}

// apiGeoMidpointHandler computes the geographic midpoint between two
// coordinates.
func apiGeoMidpointHandler(w http.ResponseWriter, r *http.Request) {
	lat1, lon1, lat2, lon2, err := parseGeoCoordinateParams(r.URL.Query())
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COORDINATES", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, geoService.Midpoint(lat1, lon1, lat2, lon2))
}

// apiMathCalculateHandler dispatches to a math.Service operation selected
// by ?operation=, composing the existing named methods rather than
// evaluating a generic expression (no expression evaluator exists or may
// be invented).
func apiMathCalculateHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	operation := strings.ToLower(q.Get("operation"))
	if operation == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_OPERATION", "operation query parameter is required", nil)
		return
	}

	parseFloatParam := func(name string) (float64, bool) {
		raw := q.Get(name)
		if raw == "" {
			return 0, false
		}
		v, err := strconv.ParseFloat(raw, 64)
		return v, err == nil
	}
	parseIntParam := func(name string) (int64, bool) {
		raw := q.Get(name)
		if raw == "" {
			return 0, false
		}
		v, err := strconv.ParseInt(raw, 10, 64)
		return v, err == nil
	}

	switch operation {
	case "add", "subtract", "multiply", "divide", "power", "percentage_of", "percentage_change":
		a, aOK := parseFloatParam("a")
		b, bOK := parseFloatParam("b")
		if !aOK || !bOK {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_OPERANDS", "a and b query parameters are required", nil)
			return
		}
		switch operation {
		case "add":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Add(a, b)})
		case "subtract":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Subtract(a, b)})
		case "multiply":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Multiply(a, b)})
		case "divide":
			result, err := mathService.Divide(a, b)
			if err != nil {
				writeEnvelopeError(w, http.StatusBadRequest, "DIVISION_BY_ZERO", err.Error(), nil)
				return
			}
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": result})
		case "power":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Power(a, b)})
		case "percentage_of":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.PercentageOf(a, b)})
		case "percentage_change":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.PercentageChange(a, b)})
		}
	case "modulo", "gcd", "lcm":
		a, aOK := parseIntParam("a")
		b, bOK := parseIntParam("b")
		if !aOK || !bOK {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_OPERANDS", "a and b query parameters are required", nil)
			return
		}
		switch operation {
		case "modulo":
			result, err := mathService.Modulo(a, b)
			if err != nil {
				writeEnvelopeError(w, http.StatusBadRequest, "MODULO_BY_ZERO", err.Error(), nil)
				return
			}
			writeEnvelopeOK(w, http.StatusOK, map[string]int64{"result": result})
		case "gcd":
			writeEnvelopeOK(w, http.StatusOK, map[string]int64{"result": mathService.GCD(a, b)})
		case "lcm":
			writeEnvelopeOK(w, http.StatusOK, map[string]int64{"result": mathService.LCM(a, b)})
		}
	case "sqrt", "cbrt", "abs", "round", "floor", "ceil", "log", "log10", "log2", "exp", "sin", "cos", "tan":
		n, nOK := parseFloatParam("n")
		if !nOK {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_OPERAND", "n query parameter is required", nil)
			return
		}
		switch operation {
		case "sqrt":
			result, err := mathService.SquareRoot(n)
			if err != nil {
				writeEnvelopeError(w, http.StatusBadRequest, "NEGATIVE_SQUARE_ROOT", err.Error(), nil)
				return
			}
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": result})
		case "cbrt":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.CubeRoot(n)})
		case "abs":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Abs(n)})
		case "round":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Round(n)})
		case "floor":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Floor(n)})
		case "ceil":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Ceil(n)})
		case "log":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Log(n)})
		case "log10":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Log10(n)})
		case "log2":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Log2(n)})
		case "exp":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Exp(n)})
		case "sin":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Sin(n)})
		case "cos":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Cos(n)})
		case "tan":
			writeEnvelopeOK(w, http.StatusOK, map[string]float64{"result": mathService.Tan(n)})
		}
	case "factorial":
		n, nOK := parseIntParam("n")
		if !nOK {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_OPERAND", "n query parameter is required", nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]string{"result": mathService.Factorial(n).String()})
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "UNSUPPORTED_OPERATION", "unsupported operation: "+operation, nil)
	}
}

// apiMathPrimeHandler reports whether the {n} path parameter is a prime
// number, using math.Service.IsPrime.
func apiMathPrimeHandler(w http.ResponseWriter, r *http.Request) {
	nParam := chi.URLParam(r, "n")
	n, err := strconv.ParseInt(nParam, 10, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "n must be an integer", nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"n":        n,
		"is_prime": mathService.IsPrime(n),
	})
}

// apiMathRandomHandler returns a random integer in the inclusive range
// [{min}, {max}] using math.Service.RandomInt.
func apiMathRandomHandler(w http.ResponseWriter, r *http.Request) {
	minParam := chi.URLParam(r, "min")
	maxParam := chi.URLParam(r, "max")

	minVal, err := strconv.ParseInt(minParam, 10, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "min must be an integer", nil)
		return
	}
	maxVal, err := strconv.ParseInt(maxParam, 10, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "max must be an integer", nil)
		return
	}
	if minVal > maxVal {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_RANGE", "min must be less than or equal to max", nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"min":    minVal,
		"max":    maxVal,
		"result": mathService.RandomInt(minVal, maxVal),
	})
}

// apiMathStatsHandler computes min/max/sum/average/median over the
// comma-separated ?numbers= query parameter, using the corresponding
// math.Service methods.
func apiMathStatsHandler(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("numbers")
	if raw == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_NUMBERS", "numbers query parameter is required (comma-separated)", nil)
		return
	}

	parts := strings.Split(raw, ",")
	numbers := make([]float64, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		v, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_NUMBER", "numbers must be a comma-separated list of numeric values", nil)
			return
		}
		numbers = append(numbers, v)
	}
	if len(numbers) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_NUMBERS", "numbers query parameter is required (comma-separated)", nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"count":   len(numbers),
		"min":     mathService.Min(numbers),
		"max":     mathService.Max(numbers),
		"sum":     mathService.Sum(numbers),
		"average": mathService.Average(numbers),
		"median":  mathService.Median(numbers),
	})
}

// apiMathFibonacciHandler returns the first {count} Fibonacci numbers using
// math.Service.Fibonacci; ?count= is required and must be a non-negative
// integer.
func apiMathFibonacciHandler(w http.ResponseWriter, r *http.Request) {
	countParam := r.URL.Query().Get("count")
	if countParam == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_COUNT", "count query parameter is required", nil)
		return
	}
	count, err := strconv.Atoi(countParam)
	if err != nil || count < 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COUNT", "count must be a non-negative integer", nil)
		return
	}

	values, err := mathService.Fibonacci(count)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COUNT", err.Error(), nil)
		return
	}
	sequence := make([]string, len(values))
	for i, v := range values {
		sequence[i] = v.String()
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"count":    count,
		"sequence": sequence,
	})
}

// apiMathBaseHandler converts ?number= from ?from_base= to ?to_base= using
// math.Service.BaseConvert; both bases must be between 2 and 36.
func apiMathBaseHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	number := q.Get("number")
	fromBaseParam := q.Get("from_base")
	toBaseParam := q.Get("to_base")
	if number == "" || fromBaseParam == "" || toBaseParam == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_PARAMS", "number, from_base, and to_base query parameters are required", nil)
		return
	}

	fromBase, err := strconv.Atoi(fromBaseParam)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "from_base must be an integer", nil)
		return
	}
	toBase, err := strconv.Atoi(toBaseParam)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "to_base must be an integer", nil)
		return
	}

	result, err := mathService.BaseConvert(number, fromBase, toBase)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"number":    number,
		"from_base": fromBase,
		"to_base":   toBase,
		"result":    result,
	})
}

// matrixRequest is the shared JSON body shape for apiMathMatrixHandler:
// two matrices ("a" required always, "b" required for add/multiply).
type matrixRequest struct {
	Operation string      `json:"operation"`
	A         [][]float64 `json:"a"`
	B         [][]float64 `json:"b"`
}

// apiMathMatrixHandler performs add, multiply, or determinant on the
// matrices supplied in the JSON request body, using the corresponding
// math.Service Matrix* method.
func apiMathMatrixHandler(w http.ResponseWriter, r *http.Request) {
	var req matrixRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", "request body must be JSON with operation, a, and (for add/multiply) b", nil)
		return
	}
	if len(req.A) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_MATRIX", "matrix a is required", nil)
		return
	}

	switch req.Operation {
	case "add":
		result, err := mathService.MatrixAdd(req.A, req.B)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MATRIX", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{"operation": req.Operation, "result": result})
	case "multiply":
		result, err := mathService.MatrixMultiply(req.A, req.B)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MATRIX", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{"operation": req.Operation, "result": result})
	case "determinant":
		result, err := mathService.MatrixDeterminant(req.A)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MATRIX", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{"operation": req.Operation, "result": result})
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_OPERATION", "operation must be one of: add, multiply, determinant", nil)
	}
}

// apiMathSequenceHandler generates ?count= numbers of ?type= (arithmetic or
// geometric) starting at ?start= and stepping by ?step=, using
// math.Service.Sequence.
func apiMathSequenceHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	seqType := q.Get("type")
	startParam := q.Get("start")
	stepParam := q.Get("step")
	countParam := q.Get("count")
	if seqType == "" || startParam == "" || stepParam == "" || countParam == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_PARAMS", "type, start, step, and count query parameters are required", nil)
		return
	}

	start, err := strconv.ParseFloat(startParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "start must be numeric", nil)
		return
	}
	step, err := strconv.ParseFloat(stepParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "step must be numeric", nil)
		return
	}
	count, err := strconv.Atoi(countParam)
	if err != nil || count < 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COUNT", "count must be a non-negative integer", nil)
		return
	}

	values, err := mathService.Sequence(seqType, start, step, count)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"type":     seqType,
		"start":    start,
		"step":     step,
		"count":    count,
		"sequence": values,
	})
}

// lengthConversions maps a "from-to" unit pair to the convert.Service
// bidirectional function that implements it. Only pairs actually
// exported by convert.Service are listed; unlisted pairs return
// UNSUPPORTED_UNITS rather than inventing new conversion math.
func lengthConversions(s *convert.Service) map[string]func(float64) float64 {
	return map[string]func(float64) float64{
		"ft-m":  s.FeetToMeters,
		"m-ft":  s.MetersToFeet,
		"in-cm": s.InchesToCentimeters,
		"cm-in": s.CentimetersToInches,
		"mi-km": s.MilesToKilometers,
		"km-mi": s.KilometersToMiles,
	}
}

// apiConvertLengthHandler converts {value} from {from} to {to} units
// using the existing bidirectional length-conversion pairs exported by
// convert.Service.
func apiConvertLengthHandler(w http.ResponseWriter, r *http.Request) {
	valueParam := chi.URLParam(r, "value")
	from := strings.ToLower(chi.URLParam(r, "from"))
	to := strings.ToLower(chi.URLParam(r, "to"))

	value, err := strconv.ParseFloat(valueParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "value must be numeric", nil)
		return
	}

	fn, ok := lengthConversions(convertService)[from+"-"+to]
	if !ok {
		writeEnvelopeError(w, http.StatusBadRequest, "UNSUPPORTED_UNITS", "unsupported unit pair: "+from+"-"+to, nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"value":  value,
		"from":   from,
		"to":     to,
		"result": fn(value),
	})
}

// temperatureConversions maps a "from-to" unit pair to the convert.Service
// bidirectional function that implements it. Only pairs actually exported
// by convert.Service are listed; unlisted pairs return UNSUPPORTED_UNITS
// rather than inventing new conversion math.
func temperatureConversions(s *convert.Service) map[string]func(float64) float64 {
	return map[string]func(float64) float64{
		"c-f": s.CelsiusToFahrenheit,
		"f-c": s.FahrenheitToCelsius,
		"c-k": s.CelsiusToKelvin,
		"k-c": s.KelvinToCelsius,
	}
}

// apiConvertTemperatureHandler converts {value} from {from} to {to} units
// using the existing bidirectional temperature-conversion pairs exported by
// convert.Service.
func apiConvertTemperatureHandler(w http.ResponseWriter, r *http.Request) {
	valueParam := chi.URLParam(r, "value")
	from := strings.ToLower(chi.URLParam(r, "from"))
	to := strings.ToLower(chi.URLParam(r, "to"))

	value, err := strconv.ParseFloat(valueParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "value must be numeric", nil)
		return
	}

	fn, ok := temperatureConversions(convertService)[from+"-"+to]
	if !ok {
		writeEnvelopeError(w, http.StatusBadRequest, "UNSUPPORTED_UNITS", "unsupported unit pair: "+from+"-"+to, nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"value":  value,
		"from":   from,
		"to":     to,
		"result": fn(value),
	})
}

// weightConversions maps a "from-to" unit pair to the convert.Service
// bidirectional function that implements it. Only pairs actually exported
// by convert.Service are listed; unlisted pairs return UNSUPPORTED_UNITS
// rather than inventing new conversion math.
func weightConversions(s *convert.Service) map[string]func(float64) float64 {
	return map[string]func(float64) float64{
		"lb-kg": s.PoundsToKilograms,
		"kg-lb": s.KilogramsToPounds,
		"oz-g":  s.OuncesToGrams,
		"g-oz":  s.GramsToOunces,
	}
}

// apiConvertWeightHandler converts {value} from {from} to {to} units using
// the existing bidirectional weight-conversion pairs exported by
// convert.Service.
func apiConvertWeightHandler(w http.ResponseWriter, r *http.Request) {
	valueParam := chi.URLParam(r, "value")
	from := strings.ToLower(chi.URLParam(r, "from"))
	to := strings.ToLower(chi.URLParam(r, "to"))

	value, err := strconv.ParseFloat(valueParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "value must be numeric", nil)
		return
	}

	fn, ok := weightConversions(convertService)[from+"-"+to]
	if !ok {
		writeEnvelopeError(w, http.StatusBadRequest, "UNSUPPORTED_UNITS", "unsupported unit pair: "+from+"-"+to, nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"value":  value,
		"from":   from,
		"to":     to,
		"result": fn(value),
	})
}

// volumeConversions maps a "from-to" unit pair to the convert.Service
// bidirectional function that implements it. Only pairs actually exported
// by convert.Service are listed; unlisted pairs return UNSUPPORTED_UNITS
// rather than inventing new conversion math.
func volumeConversions(s *convert.Service) map[string]func(float64) float64 {
	return map[string]func(float64) float64{
		"gal-l": s.GallonsToLiters,
		"l-gal": s.LitersToGallons,
	}
}

// apiConvertVolumeHandler converts {value} from {from} to {to} units using
// the existing bidirectional volume-conversion pairs exported by
// convert.Service.
func apiConvertVolumeHandler(w http.ResponseWriter, r *http.Request) {
	valueParam := chi.URLParam(r, "value")
	from := strings.ToLower(chi.URLParam(r, "from"))
	to := strings.ToLower(chi.URLParam(r, "to"))

	value, err := strconv.ParseFloat(valueParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "value must be numeric", nil)
		return
	}

	fn, ok := volumeConversions(convertService)[from+"-"+to]
	if !ok {
		writeEnvelopeError(w, http.StatusBadRequest, "UNSUPPORTED_UNITS", "unsupported unit pair: "+from+"-"+to, nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"value":  value,
		"from":   from,
		"to":     to,
		"result": fn(value),
	})
}

// timeConversions maps a "from-to" unit pair to the convert.Service
// bidirectional function that implements it. Only pairs actually exported
// by convert.Service are listed; unlisted pairs return UNSUPPORTED_UNITS
// rather than inventing new conversion math.
func timeConversions(s *convert.Service) map[string]func(float64) float64 {
	return map[string]func(float64) float64{
		"s-min":  s.SecondsToMinutes,
		"min-s":  s.MinutesToSeconds,
		"hr-min": s.HoursToMinutes,
		"min-hr": s.MinutesToHours,
		"day-hr": s.DaysToHours,
		"hr-day": s.HoursToDays,
	}
}

// apiConvertTimeHandler converts {value} from {from} to {to} units using
// the existing bidirectional time-conversion pairs exported by
// convert.Service.
func apiConvertTimeHandler(w http.ResponseWriter, r *http.Request) {
	valueParam := chi.URLParam(r, "value")
	from := strings.ToLower(chi.URLParam(r, "from"))
	to := strings.ToLower(chi.URLParam(r, "to"))

	value, err := strconv.ParseFloat(valueParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "value must be numeric", nil)
		return
	}

	fn, ok := timeConversions(convertService)[from+"-"+to]
	if !ok {
		writeEnvelopeError(w, http.StatusBadRequest, "UNSUPPORTED_UNITS", "unsupported unit pair: "+from+"-"+to, nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"value":  value,
		"from":   from,
		"to":     to,
		"result": fn(value),
	})
}

// apiGenerateQRHandler reports that QR-code generation is not supported.
// No QR encoder exists anywhere in the codebase (generate, image, or any
// dependency in go.mod) and authoring one from scratch would be inventing
// new business logic, which is explicitly forbidden.
func apiGenerateQRHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "QR code generation is not implemented: no QR encoder exists in src/service/generate or any dependency", nil)
}

// apiValidateEmailHandler validates the email address supplied in the
// JSON body ({"email":"..."}) or as an ?email= query parameter.
func apiValidateEmailHandler(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		var body struct {
			Email string `json:"email"`
		}
		if err := decodeJSONBody(r, &body); err == nil {
			email = body.Email
		}
	}
	if email == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_EMAIL", "email is required (JSON body or ?email= query parameter)", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"email": email,
		"valid": validateService.IsEmail(email),
	})
}

// apiValidateCreditCardHandler validates a credit card number (Luhn check)
// supplied as ?number= or a JSON {"number":"..."} body.
func apiValidateCreditCardHandler(w http.ResponseWriter, r *http.Request) {
	number := queryOrJSONField(r, "number")
	if number == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_NUMBER", "number is required (JSON body or ?number= query parameter)", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"number": number,
		"valid":  validateService.IsCreditCard(number),
	})
}

// apiValidateDomainHandler validates a domain name supplied as ?domain= or
// a JSON {"domain":"..."} body.
func apiValidateDomainHandler(w http.ResponseWriter, r *http.Request) {
	domain := queryOrJSONField(r, "domain")
	if domain == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_DOMAIN", "domain is required (JSON body or ?domain= query parameter)", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"domain": domain,
		"valid":  validateService.IsDomain(domain),
	})
}

// apiValidateIPHandler validates an IPv4 or IPv6 address supplied as ?ip=
// or a JSON {"ip":"..."} body.
func apiValidateIPHandler(w http.ResponseWriter, r *http.Request) {
	ip := queryOrJSONField(r, "ip")
	if ip == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_IP", "ip is required (JSON body or ?ip= query parameter)", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"ip":      ip,
		"valid":   validateService.IsIP(ip),
		"is_ipv4": validateService.IsIPv4(ip),
		"is_ipv6": validateService.IsIPv6(ip),
	})
}

// apiValidateJSONHandler validates that the raw request body is
// well-formed JSON.
func apiValidateJSONHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "READ_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"valid": validateService.IsJSON(string(body)),
	})
}

// apiValidateMACHandler validates a MAC address supplied as ?mac= or a
// JSON {"mac":"..."} body.
func apiValidateMACHandler(w http.ResponseWriter, r *http.Request) {
	mac := queryOrJSONField(r, "mac")
	if mac == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_MAC", "mac is required (JSON body or ?mac= query parameter)", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"mac":   mac,
		"valid": validateService.IsMAC(mac),
	})
}

// apiValidatePhoneHandler validates a phone number supplied as ?phone= or
// a JSON {"phone":"..."} body.
func apiValidatePhoneHandler(w http.ResponseWriter, r *http.Request) {
	phone := queryOrJSONField(r, "phone")
	if phone == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_PHONE", "phone is required (JSON body or ?phone= query parameter)", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"phone": phone,
		"valid": validateService.IsPhone(phone),
	})
}

// apiValidateURLHandler validates a URL supplied as ?url= or a JSON
// {"url":"..."} body.
func apiValidateURLHandler(w http.ResponseWriter, r *http.Request) {
	rawURL := queryOrJSONField(r, "url")
	if rawURL == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_URL", "url is required (JSON body or ?url= query parameter)", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"url":   rawURL,
		"valid": validateService.IsURL(rawURL),
	})
}

// apiValidateUUIDHandler validates a UUID supplied as ?uuid= or a JSON
// {"uuid":"..."} body.
func apiValidateUUIDHandler(w http.ResponseWriter, r *http.Request) {
	uuidStr := queryOrJSONField(r, "uuid")
	if uuidStr == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_UUID", "uuid is required (JSON body or ?uuid= query parameter)", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"uuid":  uuidStr,
		"valid": validateService.IsUUID(uuidStr),
	})
}

// apiValidateIBANHandler validates an IBAN supplied as ?iban= or a JSON
// {"iban":"..."} body against the ISO 13616 mod-97 checksum.
func apiValidateIBANHandler(w http.ResponseWriter, r *http.Request) {
	iban := queryOrJSONField(r, "iban")
	if iban == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_IBAN", "iban is required (JSON body or ?iban= query parameter)", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"iban":  iban,
		"valid": validateService.IsIBAN(iban),
	})
}

// apiValidateISBNHandler validates an ISBN-10 or ISBN-13 supplied as
// ?isbn= or a JSON {"isbn":"..."} body.
func apiValidateISBNHandler(w http.ResponseWriter, r *http.Request) {
	isbn := queryOrJSONField(r, "isbn")
	if isbn == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_ISBN", "isbn is required (JSON body or ?isbn= query parameter)", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"isbn":  isbn,
		"valid": validateService.IsISBN(isbn),
	})
}

// apiValidateVATHandler validates the structural format of an EU/UK/CH/NO
// VAT registration number supplied as ?vat= or a JSON {"vat":"..."} body.
func apiValidateVATHandler(w http.ResponseWriter, r *http.Request) {
	vat := queryOrJSONField(r, "vat")
	if vat == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_VAT", "vat is required (JSON body or ?vat= query parameter)", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"vat":   vat,
		"valid": validateService.IsVAT(vat),
	})
}

// apiParseJSONHandler parses the raw JSON document supplied in the
// request body into a generic map.
func apiParseJSONHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	parsed, err := parseService.ParseJSON(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, parsed)
}

// apiParseXMLHandler parses the raw XML document supplied in the request
// body into a generic map, reusing the existing parseService.ParseXML.
func apiParseXMLHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_XML", "request body must contain an XML document", nil)
		return
	}
	parsed, err := parseService.ParseXML(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_XML", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, parsed)
}

// apiParseCSVHandler parses the raw CSV document supplied in the request
// body (first row treated as headers) via parseService.ParseCSV.
func apiParseCSVHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_CSV", "request body must contain a CSV document", nil)
		return
	}
	parsed, err := parseService.ParseCSV(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_CSV", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, parsed)
}

// apiParseJWTHandler decodes (never verifies) the header and payload of a
// JSON Web Token supplied via the {token} path parameter. This reuses the
// exact same decodeJWTSegment helper as apiCryptoJWTDecodeHandler in the
// crypto category — parse and crypto both expose a JWT decode tool over the
// same underlying logic, matching the established cross-category reuse
// pattern (e.g. apiOsintIPHandler/apiGeoIPHandler).
func apiParseJWTHandler(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_JWT", "token must have three dot-separated segments (header.payload.signature)", nil)
		return
	}

	header, err := decodeJWTSegment(parts[0])
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_JWT_HEADER", err.Error(), nil)
		return
	}
	payload, err := decodeJWTSegment(parts[1])
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_JWT_PAYLOAD", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"header":    header,
		"payload":   payload,
		"signature": parts[2],
	})
}

// apiLanguageDetectHandler reports that language auto-detection is not
// supported. IDEA.md explicitly lists "language auto-detection" as a
// non-goal; src/service/language only offers code<->name lookup/listing,
// which is not detection and would misrepresent the response if reused.
func apiLanguageDetectHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "language auto-detection is a declared non-goal for this project; only language code/name lookup is supported", nil)
}

// apiLanguagePhoneticHandler returns the Soundex and Metaphone phonetic
// codes for a word supplied via ?word=.
func apiLanguagePhoneticHandler(w http.ResponseWriter, r *http.Request) {
	word := r.URL.Query().Get("word")
	if strings.TrimSpace(word) == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_WORD", "word query parameter is required", nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"word":      word,
		"soundex":   languageService.Soundex(word),
		"metaphone": languageService.Metaphone(word),
	})
}

// apiLanguageWordCountHandler returns word/character/line/sentence counts
// for text supplied via ?text= or the raw request body.
func apiLanguageWordCountHandler(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if text == "" {
		body, err := readRequestBody(r)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "TEXT_READ_FAILED", err.Error(), nil)
			return
		}
		text = string(body)
	}
	if strings.TrimSpace(text) == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_TEXT", "text query parameter or request body is required", nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, languageService.WordCount(text))
}

// apiTestHTTPHandler exercises the mock HTTP response fixture generator
// under a measured execution window. IDEA.md restricts outbound network
// calls to only the OSINT and weather tool families, so this cannot make
// a live request to an arbitrary target; it composes the existing
// mock/fixture and timing utilities instead.
func apiTestHTTPHandler(w http.ResponseWriter, r *http.Request) {
	var mockResponse map[string]interface{}
	elapsed := testService.MeasureExecutionTime(func() {
		mockResponse = testService.GenerateMockAPIResponse()
	})
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"response":    mockResponse,
		"duration_ms": elapsed.Milliseconds(),
	})
}

// testAssertRequest is the JSON body shape accepted by
// apiTestAssertHandler. Which fields are read depends on op: equal/
// not_equal use expected/actual, contains uses haystack/needle, true/
// false use value.
type testAssertRequest struct {
	Op       string      `json:"op"`
	Expected interface{} `json:"expected,omitempty"`
	Actual   interface{} `json:"actual,omitempty"`
	Haystack string      `json:"haystack,omitempty"`
	Needle   string      `json:"needle,omitempty"`
	Value    bool        `json:"value,omitempty"`
}

// apiTestAssertHandler runs one of the five test.Service assertion
// helpers (equal/not_equal/contains/true/false) against caller-supplied
// values and returns the resulting pass/fail TestResult. It dispatches
// to the existing Assert* methods rather than re-implementing the
// comparison logic, so the CLI/API behavior always matches whatever the
// underlying assertion helpers do.
func apiTestAssertHandler(w http.ResponseWriter, r *http.Request) {
	var body testAssertRequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}

	var result *test.TestResult
	switch strings.ToLower(body.Op) {
	case "equal":
		result = testService.AssertEqual(body.Expected, body.Actual)
	case "not_equal":
		result = testService.AssertNotEqual(body.Expected, body.Actual)
	case "contains":
		if body.Haystack == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_FIELDS", "haystack and needle are required for the contains op", nil)
			return
		}
		result = testService.AssertContains(body.Haystack, body.Needle)
	case "true":
		result = testService.AssertTrue(body.Value)
	case "false":
		result = testService.AssertFalse(body.Value)
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_OP", "op must be one of: equal, not_equal, contains, true, false", nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"op":      body.Op,
		"passed":  result.Passed,
		"message": result.Message,
	})
}

// apiTestFixtureHandler returns a named test fixture, dispatching to
// test.Service.GenerateFixture rather than re-implementing per-type
// fixture shapes here.
func apiTestFixtureHandler(w http.ResponseWriter, r *http.Request) {
	fixtureType := chi.URLParam(r, "type")
	if fixtureType == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_TYPE", "fixture type path parameter is required", nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"type":    fixtureType,
		"fixture": testService.GenerateFixture(fixtureType),
	})
}

// apiTestFakeDataHandler generates fake test data (email, username, or a
// full mock user) via the existing test.Service generators, selected by
// the ?type= query parameter (default "user").
func apiTestFakeDataHandler(w http.ResponseWriter, r *http.Request) {
	dataType := r.URL.Query().Get("type")
	if dataType == "" {
		dataType = "user"
	}
	prefix := r.URL.Query().Get("prefix")
	if prefix == "" {
		prefix = "test"
	}

	switch dataType {
	case "email":
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"type":  dataType,
			"email": testService.GenerateTestEmail(prefix),
		})
	case "username":
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"type":     dataType,
			"username": testService.GenerateTestUsername(prefix),
		})
	case "user":
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"type": dataType,
			"user": testService.GenerateMockUser(),
		})
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_TYPE", "type must be one of: email, username, user", nil)
	}
}

// apiOsintEmailHandler validates the {email} path parameter's format and
// checks whether its domain has mail-exchange (MX) records, composing
// validate.IsEmail, parse.ParseEmail, and osint.DNSLookup — all free,
// keyless, and already exported. No dedicated email-OSINT function
// exists in src/service/osint.
func apiOsintEmailHandler(w http.ResponseWriter, r *http.Request) {
	email := chi.URLParam(r, "email")
	if !validateService.IsEmail(email) {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_EMAIL", "not a valid email address", nil)
		return
	}
	parts, err := parseService.ParseEmail(email)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_EMAIL", err.Error(), nil)
		return
	}
	mxRecords, err := osintService.DNSLookup(parts.Domain, "MX")
	if err != nil {
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"email":          email,
			"domain":         parts.Domain,
			"valid_format":   true,
			"has_mx_records": false,
			"mx_records":     []string{},
		})
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"email":          email,
		"domain":         parts.Domain,
		"valid_format":   true,
		"has_mx_records": len(mxRecords) > 0,
		"mx_records":     mxRecords,
	})
}

// apiOsintDomainHandler performs a free, keyless WHOIS lookup for the
// {domain} path parameter via osint.WHOISLookup.
func apiOsintDomainHandler(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")
	if domain == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_DOMAIN", "domain is required", nil)
		return
	}
	info, err := osintService.WHOISLookup(domain)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "WHOIS_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, info)
}

// apiOsintIPHandler resolves geolocation/ISP intelligence for the {ip}
// path parameter via the shared osintService.IPLookup (same underlying
// implementation as apiGeoIPHandler).
func apiOsintIPHandler(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	info, err := osintService.IPLookup(ip)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IP_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, info)
}

// apiOsintCertHandler connects to the {domain} path parameter (host:443 by
// default, or host:port if a port is present) and reports the peer TLS
// certificate's details via osint.SSLInfo.
func apiOsintCertHandler(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")
	if domain == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_DOMAIN", "domain is required", nil)
		return
	}
	info, err := osintService.SSLInfo(domain)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "CERT_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, info)
}

// researchCitationRequest is the JSON body shape accepted by
// apiResearchCitationHandler: the four fields every research.Reference
// needs, plus the desired output style.
type researchCitationRequest struct {
	Title  string `json:"title"`
	Author string `json:"author"`
	Year   string `json:"year"`
	Source string `json:"source"`
	Style  string `json:"style"`
}

// apiResearchCitationHandler formats a single caller-supplied reference
// into a citation string. It reuses research.Service.GenerateBibliography
// with a one-element slice rather than re-implementing the APA/MLA/Chicago
// style switch, so the single-citation and bibliography code paths can
// never drift apart.
func apiResearchCitationHandler(w http.ResponseWriter, r *http.Request) {
	var body researchCitationRequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if body.Title == "" || body.Author == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_FIELDS", "title and author are required", nil)
		return
	}
	if body.Style == "" {
		body.Style = "APA"
	}

	citations := researchService.GenerateBibliography([]research.Reference{
		{Title: body.Title, Author: body.Author, Year: body.Year, Source: body.Source},
	}, body.Style)

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"style":    body.Style,
		"citation": citations[0],
	})
}

// apiResearchDOIHandler validates the wildcard path suffix as a DOI and
// returns its canonical resolver URL, using
// research.Service.ValidateDOI/FormatDOI. A wildcard route (rather than a
// {doi} chi.URLParam) is required because DOIs always contain at least one
// "/" (prefix/suffix, e.g. "10.1000/182"), which a single path segment
// parameter cannot capture.
func apiResearchDOIHandler(w http.ResponseWriter, r *http.Request) {
	doi := chi.URLParam(r, "*")
	if doi == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_DOI", "doi is required", nil)
		return
	}
	valid := researchService.ValidateDOI(doi)
	if !valid {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_DOI", "doi must start with \"10.\" and be at least 8 characters", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"doi":   doi,
		"valid": valid,
		"url":   researchService.FormatDOI(doi),
	})
}

// apiResearchExtractHandler reports that citation extraction from
// unstructured text is not supported. research.go's own source comment
// documents this as unimplemented ("Full research service could include:
// citation extraction from text") — only pre-structured citation
// formatting (APA/MLA/Chicago) and DOI validation exist.
func apiResearchExtractHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "citation/reference extraction from unstructured text is not implemented; only formatting of caller-supplied citation fields is supported", nil)
}

// apiFunJokeHandler composes the existing joke-category selector with a
// fortune-cookie string, since no dedicated joke-text corpus exists
// anywhere in src/service/fun (RandomJokeType only returns a category
// label such as "dad joke", never joke text).
func apiFunJokeHandler(w http.ResponseWriter, r *http.Request) {
	jokeType, err := funService.RandomJokeType()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "JOKE_GENERATION_FAILED", err.Error(), nil)
		return
	}
	text, err := funService.Fortune()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "JOKE_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"type": jokeType,
		"text": text,
	})
}

// apiFunFortuneHandler returns a single random fortune-cookie style saying
// from funService.Fortune, independent of the joke-type composite endpoint.
func apiFunFortuneHandler(w http.ResponseWriter, r *http.Request) {
	text, err := funService.Fortune()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "FORTUNE_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"text": text})
}

// apiLoremPersonHandler generates a fake person (name/email/phone).
func apiLoremPersonHandler(w http.ResponseWriter, r *http.Request) {
	person, err := loremService.Person()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, person)
}

// apiLoremAddressHandler generates a fake street address.
func apiLoremAddressHandler(w http.ResponseWriter, r *http.Request) {
	address, err := loremService.Address()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, address)
}

// apiLoremCompanyHandler generates a fake company name and catchphrase.
func apiLoremCompanyHandler(w http.ResponseWriter, r *http.Request) {
	company, err := loremService.Company()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, company)
}

// apiDevFormatJSONHandler pretty-prints the raw JSON document supplied in
// the request body.
// apiDevBase64Handler encodes or decodes the raw request body as base64
// (standard or URL-safe, per ?urlsafe=) depending on the ?action= query
// parameter (encode, the default, or decode).
func apiDevBase64Handler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	action := r.URL.Query().Get("action")
	if action == "" {
		action = "encode"
	}
	urlSafe := config.IsTruthy(r.URL.Query().Get("urlsafe"))

	switch action {
	case "encode":
		result := text.Base64Encode(string(raw))
		if urlSafe {
			result = text.Base64URLEncode(string(raw))
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]string{"result": result})
	case "decode":
		var (
			result string
			err    error
		)
		if urlSafe {
			result, err = text.Base64URLDecode(string(raw))
		} else {
			result, err = text.Base64Decode(string(raw))
		}
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BASE64", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]string{"result": result})
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_ACTION", "action must be encode or decode", nil)
	}
}

// apiDevURLEncodeHandler URL-encodes or URL-decodes the raw request body
// depending on the ?action= query parameter (encode, the default, or
// decode).
func apiDevURLEncodeHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	action := r.URL.Query().Get("action")
	if action == "" {
		action = "encode"
	}

	switch action {
	case "encode":
		writeEnvelopeOK(w, http.StatusOK, map[string]string{"result": text.URLEncode(string(raw))})
	case "decode":
		result, err := text.URLDecode(string(raw))
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_URL_ENCODING", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]string{"result": result})
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_ACTION", "action must be encode or decode", nil)
	}
}

func apiDevFormatJSONHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	formatted, err := devService.FormatJSON(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"formatted": formatted})
}

// apiImagePlaceholderHandler generates a placeholder image of
// {width}x{height} and writes it as raw binary content. PART 14's JSON
// envelope is scoped to application/json bodies; a binary image payload
// is served directly with the matching Content-Type instead.
func apiImagePlaceholderHandler(w http.ResponseWriter, r *http.Request) {
	widthParam := chi.URLParam(r, "width")
	heightParam := chi.URLParam(r, "height")

	width, err := strconv.Atoi(widthParam)
	if err != nil || width <= 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_WIDTH", "width must be a positive integer", nil)
		return
	}
	height, err := strconv.Atoi(heightParam)
	if err != nil || height <= 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_HEIGHT", "height must be a positive integer", nil)
		return
	}

	format := r.URL.Query().Get("format")
	bgColor := r.URL.Query().Get("bg")
	if bgColor == "" {
		bgColor = "#CCCCCC"
	}

	data, err := imageService.GeneratePlaceholder(width, height, format, bgColor)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_GENERATION_FAILED", err.Error(), nil)
		return
	}

	contentType := "image/png"
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		contentType = "image/jpeg"
	case "gif":
		contentType = "image/gif"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// imageContentType maps a decoded/output format name to its MIME type,
// defaulting to PNG when the format is empty or unrecognized.
func imageContentType(format string) string {
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	default:
		return "image/png"
	}
}

// readUploadedImage reads the source image for the resize/crop/metadata/
// convert handlers. It accepts either a multipart/form-data upload (field
// name "image", matching the frontend tool-page forms) or a raw binary
// request body (curl --data-binary), so the same endpoint serves both the
// browser form and direct API callers.
func readUploadedImage(r *http.Request) ([]byte, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(strings.ToLower(contentType), "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			return nil, fmt.Errorf("failed to parse uploaded image: %w", err)
		}
		file, _, err := r.FormFile("image")
		if err != nil {
			return nil, fmt.Errorf("image file is required")
		}
		defer file.Close()
		return io.ReadAll(io.LimitReader(file, 1<<20))
	}
	return readRequestBody(r)
}

// apiImageResizeHandler decodes an uploaded image and returns it resized
// to the requested width x height, in the requested (or original) format.
func apiImageResizeHandler(w http.ResponseWriter, r *http.Request) {
	data, err := readUploadedImage(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_READ_FAILED", err.Error(), nil)
		return
	}

	width, err := strconv.Atoi(r.FormValue("width"))
	if err != nil || width <= 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_WIDTH", "width must be a positive integer", nil)
		return
	}
	height, err := strconv.Atoi(r.FormValue("height"))
	if err != nil || height <= 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_HEIGHT", "height must be a positive integer", nil)
		return
	}

	svc := image.New()
	if err := svc.Load(data); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_DECODE_FAILED", err.Error(), nil)
		return
	}
	if err := svc.Resize(width, height); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_RESIZE_FAILED", err.Error(), nil)
		return
	}

	format := r.FormValue("format")
	out, err := svc.Bytes(orDefaultOutputFormat(format))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_ENCODE_FAILED", err.Error(), nil)
		return
	}

	w.Header().Set("Content-Type", imageContentType(format))
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}

// apiImageCropHandler decodes an uploaded image and returns the
// x,y,width,height region cropped from it, in the requested (or original)
// format.
func apiImageCropHandler(w http.ResponseWriter, r *http.Request) {
	data, err := readUploadedImage(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_READ_FAILED", err.Error(), nil)
		return
	}

	x, err := strconv.Atoi(r.FormValue("x"))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_X", "x must be an integer", nil)
		return
	}
	y, err := strconv.Atoi(r.FormValue("y"))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_Y", "y must be an integer", nil)
		return
	}
	width, err := strconv.Atoi(r.FormValue("width"))
	if err != nil || width <= 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_WIDTH", "width must be a positive integer", nil)
		return
	}
	height, err := strconv.Atoi(r.FormValue("height"))
	if err != nil || height <= 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_HEIGHT", "height must be a positive integer", nil)
		return
	}

	svc := image.New()
	if err := svc.Load(data); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_DECODE_FAILED", err.Error(), nil)
		return
	}
	if err := svc.Crop(x, y, width, height); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_CROP_FAILED", err.Error(), nil)
		return
	}

	format := r.FormValue("format")
	out, err := svc.Bytes(orDefaultOutputFormat(format))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_ENCODE_FAILED", err.Error(), nil)
		return
	}

	w.Header().Set("Content-Type", imageContentType(format))
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}

// apiImageMetadataHandler decodes an uploaded image and reports its
// dimensions, format, and byte size as JSON.
func apiImageMetadataHandler(w http.ResponseWriter, r *http.Request) {
	data, err := readUploadedImage(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_READ_FAILED", err.Error(), nil)
		return
	}

	svc := image.New()
	if err := svc.Load(data); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_DECODE_FAILED", err.Error(), nil)
		return
	}

	bounds := svc.Bounds()
	writeEnvelopeOK(w, http.StatusOK, image.ImageInfo{
		Width:  bounds.Dx(),
		Height: bounds.Dy(),
		Format: svc.Format(),
		Size:   int64(len(data)),
	})
}

// apiImageConvertHandler decodes an uploaded image and re-encodes it in
// the requested output format.
func apiImageConvertHandler(w http.ResponseWriter, r *http.Request) {
	data, err := readUploadedImage(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_READ_FAILED", err.Error(), nil)
		return
	}

	format := r.FormValue("format")
	if format == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_FORMAT", "format query parameter is required", nil)
		return
	}

	svc := image.New()
	if err := svc.Load(data); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_DECODE_FAILED", err.Error(), nil)
		return
	}

	out, err := svc.Bytes(format)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_ENCODE_FAILED", err.Error(), nil)
		return
	}

	w.Header().Set("Content-Type", imageContentType(format))
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}

// orDefaultOutputFormat returns format unchanged when set, otherwise "png",
// matching image.Service.Bytes' supported format set.
func orDefaultOutputFormat(format string) string {
	if format == "" {
		return "png"
	}
	return format
}

// decodeJWTSegment base64url-decodes a single JWT segment (header or
// payload) and parses it as JSON, tolerating both padded and unpadded
// base64url encoding as produced by different JWT libraries.
func decodeJWTSegment(segment string) (map[string]interface{}, error) {
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(segment)
		if err != nil {
			return nil, fmt.Errorf("invalid base64url encoding")
		}
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("invalid JSON in segment")
	}
	return decoded, nil
}

// apiCryptoJWTDecodeHandler decodes (never verifies) the header and payload
// of a JSON Web Token. No signature verification is performed — this is a
// read-only inspection tool, matching the JWT decoder's stated purpose of
// viewing header/payload/signature details, not validating trust.
func apiCryptoJWTDecodeHandler(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_JWT", "token must have three dot-separated segments (header.payload.signature)", nil)
		return
	}

	header, err := decodeJWTSegment(parts[0])
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_JWT_HEADER", err.Error(), nil)
		return
	}
	payload, err := decodeJWTSegment(parts[1])
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_JWT_PAYLOAD", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"header":    header,
		"payload":   payload,
		"signature": parts[2],
	})
}

// apiNetworkDNSHandler queries DNS records for a domain via the system
// resolver, composing the existing free/keyless osint.DNSLookup function.
// Defaults to an A-record lookup when no record type is given.
func apiNetworkDNSHandler(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")
	recordType := chi.URLParam(r, "type")
	if recordType == "" {
		recordType = "A"
	}

	records, err := osintService.DNSLookup(domain, recordType)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "DNS_LOOKUP_FAILED", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"domain":  domain,
		"type":    strings.ToUpper(recordType),
		"records": records,
	})
}

// textCompressRequest is the JSON body shape accepted by
// apiTextCompressHandler: the input data, the algorithm (gzip, zlib, or
// flate/deflate), and the direction (compress or decompress).
type textCompressRequest struct {
	Data      string `json:"data"`
	Algorithm string `json:"algorithm"`
	Mode      string `json:"mode"`
}

// apiTextCompressHandler compresses or decompresses data using
// text.Compress/text.Decompress, both base64-encoded on the wire so the
// result is always safe JSON. Mode defaults to "compress"; Algorithm
// defaults to "gzip" (zlib and flate/deflate are also supported by the
// underlying service — brotli/zstd are not implemented).
func apiTextCompressHandler(w http.ResponseWriter, r *http.Request) {
	var body textCompressRequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if body.Data == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_DATA", "data is required", nil)
		return
	}
	if body.Algorithm == "" {
		body.Algorithm = "gzip"
	}
	if body.Mode == "" {
		body.Mode = "compress"
	}

	var result string
	var err error
	switch strings.ToLower(body.Mode) {
	case "compress":
		result, err = text.Compress(body.Data, body.Algorithm)
	case "decompress":
		result, err = text.Decompress(body.Data, body.Algorithm)
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MODE", "mode must be compress or decompress", nil)
		return
	}
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "COMPRESS_FAILED", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"mode":      body.Mode,
		"algorithm": body.Algorithm,
		"result":    result,
	})
}

// textDiffRequest is the JSON body shape accepted by apiTextDiffHandler.
type textDiffRequest struct {
	Text1 string `json:"text1"`
	Text2 string `json:"text2"`
}

// apiTextDiffHandler returns a unified line diff between two texts using
// text.Diff.
func apiTextDiffHandler(w http.ResponseWriter, r *http.Request) {
	var body textDiffRequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"diff": text.Diff(body.Text1, body.Text2),
	})
}

// textExtractRequest is the JSON body shape accepted by
// apiTextExtractHandler: the source text and which kind of token to pull
// out of it.
type textExtractRequest struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

// apiTextExtractHandler pulls emails, URLs, IPs, or phone numbers out of
// free-form text using the matching text.Extract* function.
func apiTextExtractHandler(w http.ResponseWriter, r *http.Request) {
	var body textExtractRequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if body.Type == "" {
		body.Type = "emails"
	}

	var matches []string
	switch strings.ToLower(body.Type) {
	case "emails", "email":
		matches = text.ExtractEmails(body.Text)
	case "urls", "url":
		matches = text.ExtractURLs(body.Text)
	case "ips", "ip":
		matches = text.ExtractIPs(body.Text)
	case "phones", "phone":
		matches = text.ExtractPhones(body.Text)
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_TYPE", "type must be emails, urls, ips, or phones", nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"type":    body.Type,
		"matches": matches,
	})
}

// apiTextNanoIDHandler returns a newly generated NanoID via text.NanoID.
func apiTextNanoIDHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"nanoid": text.NanoID(),
	})
}

// apiTextULIDHandler returns a newly generated ULID via text.ULID.
func apiTextULIDHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"ulid": text.ULID(),
	})
}

// textRegexRequest is the JSON body shape accepted by apiTextRegexHandler,
// shared by both the /text/regex and /dev/regex tool pages.
type textRegexRequest struct {
	Pattern     string `json:"pattern"`
	Text        string `json:"text"`
	Replacement string `json:"replacement"`
	Mode        string `json:"mode"`
}

// apiTextRegexHandler tests a regular expression against input text. Mode
// "match" (the default) returns every match via text.RegexMatch; mode
// "replace" substitutes Replacement via text.RegexReplace; mode "explain"
// returns a human-readable breakdown via text.RegexExplain.
func apiTextRegexHandler(w http.ResponseWriter, r *http.Request) {
	var body textRegexRequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if body.Pattern == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_PATTERN", "pattern is required", nil)
		return
	}
	if body.Mode == "" {
		body.Mode = "match"
	}

	switch strings.ToLower(body.Mode) {
	case "match":
		matches, err := text.RegexMatch(body.Pattern, body.Text)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_PATTERN", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"mode":    body.Mode,
			"matches": matches,
		})
	case "replace":
		result, err := text.RegexReplace(body.Pattern, body.Text, body.Replacement)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_PATTERN", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"mode":   body.Mode,
			"result": result,
		})
	case "explain":
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"mode":        body.Mode,
			"explanation": text.RegexExplain(body.Pattern),
		})
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MODE", "mode must be match, replace, or explain", nil)
	}
}

// cryptoEncryptRequest is the JSON body shape accepted by
// apiCryptoEncryptHandler and apiCryptoDecryptHandler.
type cryptoEncryptRequest struct {
	Plaintext  string `json:"plaintext"`
	Ciphertext string `json:"ciphertext"`
	Key        string `json:"key"`
}

// apiCryptoEncryptHandler encrypts plaintext with AES-256-GCM using a key
// derived from the supplied passphrase via Argon2id, composing
// crypto.AESEncrypt.
func apiCryptoEncryptHandler(w http.ResponseWriter, r *http.Request) {
	var body cryptoEncryptRequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if body.Plaintext == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_PLAINTEXT", "plaintext is required", nil)
		return
	}
	if body.Key == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_KEY", "key is required", nil)
		return
	}

	ciphertext, err := crypto.AESEncrypt(body.Plaintext, body.Key)
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "ENCRYPT_FAILED", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"ciphertext": ciphertext,
		"algorithm":  "AES-256-GCM",
	})
}

// apiCryptoDecryptHandler decrypts a base64-encoded AES-256-GCM payload
// produced by apiCryptoEncryptHandler, composing crypto.AESDecrypt.
func apiCryptoDecryptHandler(w http.ResponseWriter, r *http.Request) {
	var body cryptoEncryptRequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if body.Ciphertext == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_CIPHERTEXT", "ciphertext is required", nil)
		return
	}
	if body.Key == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_KEY", "key is required", nil)
		return
	}

	plaintext, err := crypto.AESDecrypt(body.Ciphertext, body.Key)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "DECRYPT_FAILED", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"plaintext": plaintext,
	})
}

// cryptoRSARequest is the JSON body shape accepted by apiCryptoRSAHandler.
type cryptoRSARequest struct {
	Mode       string `json:"mode"`
	Plaintext  string `json:"plaintext"`
	Ciphertext string `json:"ciphertext"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	Bits       int    `json:"bits"`
}

// apiCryptoRSAHandler handles RSA keypair generation, RSA-OAEP encryption,
// and RSA-OAEP decryption in one endpoint, selected by Mode. Composes
// crypto.GenerateRSAKeys, crypto.RSAEncrypt, and crypto.RSADecrypt.
func apiCryptoRSAHandler(w http.ResponseWriter, r *http.Request) {
	var body cryptoRSARequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if body.Mode == "" {
		body.Mode = "generate"
	}

	switch strings.ToLower(body.Mode) {
	case "generate":
		bits := body.Bits
		if bits == 0 {
			bits = 2048
		}
		privateKey, publicKey, err := crypto.GenerateRSAKeys(bits)
		if err != nil {
			writeEnvelopeError(w, http.StatusInternalServerError, "GENERATE_FAILED", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"mode":        body.Mode,
			"private_key": privateKey,
			"public_key":  publicKey,
			"bits":        bits,
		})
	case "encrypt":
		if body.Plaintext == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_PLAINTEXT", "plaintext is required", nil)
			return
		}
		if body.PublicKey == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_PUBLIC_KEY", "public_key is required", nil)
			return
		}
		ciphertext, err := crypto.RSAEncrypt(body.Plaintext, body.PublicKey)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "ENCRYPT_FAILED", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"mode":       body.Mode,
			"ciphertext": ciphertext,
		})
	case "decrypt":
		if body.Ciphertext == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_CIPHERTEXT", "ciphertext is required", nil)
			return
		}
		if body.PrivateKey == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_PRIVATE_KEY", "private_key is required", nil)
			return
		}
		plaintext, err := crypto.RSADecrypt(body.Ciphertext, body.PrivateKey)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "DECRYPT_FAILED", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"mode":      body.Mode,
			"plaintext": plaintext,
		})
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MODE", "mode must be generate, encrypt, or decrypt", nil)
	}
}

// cryptoHMACRequest is the JSON body shape accepted by apiCryptoHMACHandler.
type cryptoHMACRequest struct {
	Algorithm string `json:"algorithm"`
	Key       string `json:"key"`
	Message   string `json:"message"`
}

// apiCryptoHMACHandler computes an HMAC of Message using Key, composing
// crypto.HMACGenerate. Algorithm defaults to sha256; sha1 is also
// supported.
func apiCryptoHMACHandler(w http.ResponseWriter, r *http.Request) {
	var body cryptoHMACRequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if body.Key == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_KEY", "key is required", nil)
		return
	}
	if body.Algorithm == "" {
		body.Algorithm = "sha256"
	}

	mac, err := crypto.HMACGenerate(body.Algorithm, body.Key, body.Message)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_ALGORITHM", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"algorithm": strings.ToLower(body.Algorithm),
		"hmac":      mac,
	})
}
