package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/service/convert"
	"github.com/apimgr/api/src/service/dev"
	"github.com/apimgr/api/src/service/docker"
	"github.com/apimgr/api/src/service/fun"
	"github.com/apimgr/api/src/service/image"
	"github.com/apimgr/api/src/service/lorem"
	"github.com/apimgr/api/src/service/math"
	"github.com/apimgr/api/src/service/osint"
	"github.com/apimgr/api/src/service/parse"
	"github.com/apimgr/api/src/service/research"
	"github.com/apimgr/api/src/service/test"
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
	researchService = research.New()
	funService      = fun.New()
	loremService    = lorem.New()
	devService      = dev.New()
	imageService    = image.New()
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

// apiLanguageDetectHandler reports that language auto-detection is not
// supported. IDEA.md explicitly lists "language auto-detection" as a
// non-goal; src/service/language only offers code<->name lookup/listing,
// which is not detection and would misrepresent the response if reused.
func apiLanguageDetectHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "language auto-detection is a declared non-goal for this project; only language code/name lookup is supported", nil)
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

// apiLoremPersonHandler generates a fake person (name/email/phone).
func apiLoremPersonHandler(w http.ResponseWriter, r *http.Request) {
	person, err := loremService.Person()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, person)
}

// apiDevFormatJSONHandler pretty-prints the raw JSON document supplied in
// the request body.
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
