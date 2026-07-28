package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/service/convert"
	"github.com/apimgr/api/src/service/crypto"
	"github.com/apimgr/api/src/service/datetime"
	"github.com/apimgr/api/src/service/dev"
	"github.com/apimgr/api/src/service/docker"
	"github.com/apimgr/api/src/service/fun"
	"github.com/apimgr/api/src/service/generate"
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
	generateService = generate.New()
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

// apiDockerLintHandler lints the Dockerfile text supplied in the request
// body for common anti-patterns, using docker.Service.LintDockerfile.
func apiDockerLintHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_DOCKERFILE", "request body must contain Dockerfile content", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, dockerService.LintDockerfile(string(raw)))
}

// apiDockerBestPracticesHandler returns the static curated Docker best
// practices guide, using docker.Service.BestPracticesGuide.
func apiDockerBestPracticesHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"tips": dockerService.BestPracticesGuide(),
	})
}

// apiDockerComposeValidateHandler validates the docker-compose YAML text
// supplied in the request body, using docker.Service.ValidateCompose.
func apiDockerComposeValidateHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_COMPOSE", "request body must contain docker-compose YAML content", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, dockerService.ValidateCompose(string(raw)))
}

// apiDockerComposeToRunHandler converts the docker-compose YAML text
// supplied in the request body into an equivalent docker run command for
// the service named by ?service= (or the file's only service when
// omitted), using docker.Service.ComposeToRunCommand.
func apiDockerComposeToRunHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_COMPOSE", "request body must contain docker-compose YAML content", nil)
		return
	}
	cmd, err := dockerService.ComposeToRunCommand(string(raw), r.URL.Query().Get("service"))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COMPOSE", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"command": cmd})
}

// apiDockerRunToComposeHandler converts the docker run command line
// supplied in the request body into an equivalent docker-compose service
// block, using docker.Service.RunCommandToCompose.
func apiDockerRunToComposeHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_COMMAND", "request body must contain a docker run command", nil)
		return
	}
	compose, err := dockerService.RunCommandToCompose(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COMMAND", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"compose": compose})
}

// apiDockerEnvParserHandler parses the .env file text supplied in the
// request body into structured key/value entries, using
// docker.Service.ParseEnvFile.
func apiDockerEnvParserHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_ENV", "request body must contain .env file content", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, dockerService.ParseEnvFile(string(raw)))
}

// apiDockerNetworkHelperHandler generates a docker network create command
// and a matching compose networks: block from query-string parameters,
// using docker.Service.GenerateNetworkConfig.
func apiDockerNetworkHelperHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cfg := docker.NetworkHelperConfig{
		Name:     q.Get("name"),
		Driver:   q.Get("driver"),
		Subnet:   q.Get("subnet"),
		Gateway:  q.Get("gateway"),
		Internal: config.IsTruthy(q.Get("internal")),
	}
	result, err := dockerService.GenerateNetworkConfig(cfg)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_NETWORK_CONFIG", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiDockerSecurityScanHandler statically scans the Dockerfile or compose
// text supplied in the request body for common security anti-patterns,
// using docker.Service.ScanSecurity.
func apiDockerSecurityScanHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_CONTENT", "request body must contain Dockerfile or compose content", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, dockerService.ScanSecurity(string(raw)))
}

// apiDockerSizeOptimizerHandler statically analyzes the Dockerfile text
// supplied in the request body and suggests image-size reduction changes,
// using docker.Service.OptimizeSize.
func apiDockerSizeOptimizerHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_DOCKERFILE", "request body must contain Dockerfile content", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, dockerService.OptimizeSize(string(raw)))
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

// apiWeatherAirQualityHandler returns current air quality (AQI and
// pollutant concentrations) for the {location} path parameter.
func apiWeatherAirQualityHandler(w http.ResponseWriter, r *http.Request) {
	location := chi.URLParam(r, "location")
	data, err := weatherService.GetAirQuality(location)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "WEATHER_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, data)
}

// apiWeatherAlertsHandler returns active government weather alerts for the
// {location} path parameter, aggregated across NWS (US), Environment
// Canada (CA), and MeteoAlarm (Europe), normalized into a uniform shape.
func apiWeatherAlertsHandler(w http.ResponseWriter, r *http.Request) {
	location := chi.URLParam(r, "location")
	alerts, err := weatherService.GetAlerts(location)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "WEATHER_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"location": location,
		"alerts":   alerts,
	})
}

// apiWeatherAstronomyHandler returns sunrise/sunset and daylight data for
// the {location} path parameter.
func apiWeatherAstronomyHandler(w http.ResponseWriter, r *http.Request) {
	location := chi.URLParam(r, "location")
	data, err := weatherService.GetAstronomy(location)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "WEATHER_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, data)
}

// apiWeatherHistoricalHandler returns historical daily weather for the
// {location} path parameter between the required ?start= and ?end= query
// parameters (each YYYY-MM-DD).
func apiWeatherHistoricalHandler(w http.ResponseWriter, r *http.Request) {
	location := chi.URLParam(r, "location")
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")
	if start == "" || end == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_DATE_RANGE", "start and end query parameters are required (YYYY-MM-DD)", nil)
		return
	}
	data, err := weatherService.GetHistorical(location, start, end)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "WEATHER_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"location": location,
		"start":    start,
		"end":      end,
		"days":     data,
	})
}

// apiWeatherHourlyHandler returns an hourly weather forecast for the
// {location} path parameter. The number of hours (1-48) is read from the
// ?hours= query parameter, defaulting to 24.
func apiWeatherHourlyHandler(w http.ResponseWriter, r *http.Request) {
	location := chi.URLParam(r, "location")
	hours := 24
	if raw := r.URL.Query().Get("hours"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_HOURS", "hours must be an integer between 1 and 48", nil)
			return
		}
		hours = parsed
	}
	data, err := weatherService.GetHourly(location, hours)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "WEATHER_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"location": location,
		"hours":    hours,
		"hourly":   data,
	})
}

// apiWeatherMapsHandler is a permanent gap: keyless weather tile/map
// imagery has no free provider within IDEA.md's outbound-call boundary.
func apiWeatherMapsHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "weather map tile imagery requires a keyed provider (e.g. RainViewer, OpenWeatherMap tiles); no free keyless provider exists within this project's declared outbound-call boundary; not supported", nil)
}

// apiWeatherMarineHandler returns current marine/ocean conditions for the
// {location} path parameter. Inland locations return zero-value fields,
// not an error.
func apiWeatherMarineHandler(w http.ResponseWriter, r *http.Request) {
	location := chi.URLParam(r, "location")
	data, err := weatherService.GetMarine(location)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "WEATHER_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, data)
}

// apiWeatherPollenHandler returns current pollen counts for the {location}
// path parameter. Coverage is currently limited to Europe by the
// upstream provider; other regions return an explanatory coverage note.
func apiWeatherPollenHandler(w http.ResponseWriter, r *http.Request) {
	location := chi.URLParam(r, "location")
	data, err := weatherService.GetPollen(location)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "WEATHER_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, data)
}

// apiWeatherRadarHandler is a permanent gap: keyless weather radar imagery
// has no free provider within IDEA.md's outbound-call boundary.
func apiWeatherRadarHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "weather radar imagery requires a keyed provider (e.g. RainViewer, NOAA radar mosaics); no free keyless provider exists within this project's declared outbound-call boundary; not supported", nil)
}

// apiWeatherUVHandler returns the current UV index for the {location}
// path parameter.
func apiWeatherUVHandler(w http.ResponseWriter, r *http.Request) {
	location := chi.URLParam(r, "location")
	data, err := weatherService.GetUVIndex(location)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "WEATHER_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, data)
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

// parseGeoSingleCoordinateParams parses the lat/lon query parameters shared
// by the single-point geo.Service operations (reverse, timezone, geohash,
// h3, plus code), returning a descriptive error if either is missing or
// invalid.
func parseGeoSingleCoordinateParams(q url.Values) (lat, lon float64, err error) {
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

	if lat, err = parse("lat"); err != nil {
		return
	}
	if lon, err = parse("lon"); err != nil {
		return
	}
	return
}

// apiGeoGeocodeHandler converts an address or place name to coordinates
// using the free, keyless Nominatim (OpenStreetMap) search API.
func apiGeoGeocodeHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "QUERY_REQUIRED", "q query parameter is required", nil)
		return
	}

	results, err := geoService.Geocode(query)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "UPSTREAM_ERROR", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"query":   query,
		"results": results,
	})
}

// apiGeoReverseHandler converts coordinates to a human-readable address
// using the free, keyless Nominatim (OpenStreetMap) reverse geocoding API.
func apiGeoReverseHandler(w http.ResponseWriter, r *http.Request) {
	lat, lon, err := parseGeoSingleCoordinateParams(r.URL.Query())
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COORDINATES", err.Error(), nil)
		return
	}

	result, err := geoService.ReverseGeocode(lat, lon)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "UPSTREAM_ERROR", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiGeoTimezoneHandler resolves the IANA timezone name for a coordinate
// using the free, keyless Open-Meteo forecast API's timezone=auto
// resolution.
func apiGeoTimezoneHandler(w http.ResponseWriter, r *http.Request) {
	lat, lon, err := parseGeoSingleCoordinateParams(r.URL.Query())
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COORDINATES", err.Error(), nil)
		return
	}

	result, err := geoService.Timezone(lat, lon)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "UPSTREAM_ERROR", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiGeoCountryHandler resolves country reference data (name, alpha-2/
// alpha-3/numeric codes, capital, currency, calling code, TLD, region) from
// a country name, alpha-2 code, or alpha-3 code.
func apiGeoCountryHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "QUERY_REQUIRED", "q query parameter is required", nil)
		return
	}

	info, err := geoService.Country(query)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "COUNTRY_NOT_FOUND", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, info)
}

// apiGeoGeohashHandler encodes a coordinate to a base32 geohash (?lat/?lon,
// optional ?precision, default 9), or decodes a geohash back to a
// coordinate (?hash).
func apiGeoGeohashHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	if hash := q.Get("hash"); hash != "" {
		coord, err := geoService.GeohashDecode(hash)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_GEOHASH", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"geohash":    hash,
			"coordinate": coord,
		})
		return
	}

	lat, lon, err := parseGeoSingleCoordinateParams(q)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COORDINATES", err.Error(), nil)
		return
	}

	precision := 9
	if raw := q.Get("precision"); raw != "" {
		v, convErr := strconv.Atoi(raw)
		if convErr != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_PRECISION", "precision must be an integer", nil)
			return
		}
		precision = v
	}

	hash, err := geoService.GeohashEncode(lat, lon, precision)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_PARAMETERS", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"geohash":   hash,
		"latitude":  lat,
		"longitude": lon,
		"precision": precision,
	})
}

// apiGeoH3Handler encodes a coordinate to an Uber H3 hexagonal cell index
// (?lat/?lon, optional ?resolution, default 9).
func apiGeoH3Handler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, lon, err := parseGeoSingleCoordinateParams(q)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COORDINATES", err.Error(), nil)
		return
	}

	resolution := 9
	if raw := q.Get("resolution"); raw != "" {
		v, convErr := strconv.Atoi(raw)
		if convErr != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_RESOLUTION", "resolution must be an integer", nil)
			return
		}
		resolution = v
	}

	result, err := geoService.H3Encode(lat, lon, resolution)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_PARAMETERS", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiGeoPlusCodeHandler encodes a coordinate to a Google Open Location Code
// (?lat/?lon), or decodes a plus code back to a coordinate (?code).
func apiGeoPlusCodeHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	if code := q.Get("code"); code != "" {
		coord, err := geoService.PlusCodeDecode(code)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_PLUS_CODE", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"code":       code,
			"coordinate": coord,
		})
		return
	}

	lat, lon, err := parseGeoSingleCoordinateParams(q)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COORDINATES", err.Error(), nil)
		return
	}

	result, err := geoService.PlusCodeEncode(lat, lon)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_PARAMETERS", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiGeoBBoxHandler computes a bounding box either from a center coordinate
// plus radius in kilometers (?lat/?lon/?radius) or from a list of
// coordinates (?coords=lat1,lon1|lat2,lon2|...).
func apiGeoBBoxHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	if coordsParam := q.Get("coords"); coordsParam != "" {
		pairs := strings.Split(coordsParam, "|")
		coords := make([]geo.Coordinate, 0, len(pairs))
		for _, pair := range pairs {
			parts := strings.Split(pair, ",")
			if len(parts) != 2 {
				writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COORDS", "each coords entry must be lat,lon", nil)
				return
			}
			lat, latErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			lon, lonErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if latErr != nil || lonErr != nil {
				writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COORDS", "each coords entry must be numeric lat,lon", nil)
				return
			}
			coords = append(coords, geo.Coordinate{Latitude: lat, Longitude: lon})
		}

		box, err := geoService.BoundingBoxFromCoordinates(coords)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_PARAMETERS", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, box)
		return
	}

	lat, lon, err := parseGeoSingleCoordinateParams(q)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COORDINATES", err.Error(), nil)
		return
	}

	radiusRaw := q.Get("radius")
	if radiusRaw == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "RADIUS_REQUIRED", "radius query parameter is required", nil)
		return
	}
	radius, err := strconv.ParseFloat(radiusRaw, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_RADIUS", "radius must be a number", nil)
		return
	}

	box, err := geoService.BoundingBoxFromRadius(lat, lon, radius)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_PARAMETERS", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, box)
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

// areaConversions maps a "from-to" unit pair to the convert.Service
// bidirectional function that implements it. Only pairs actually exported
// by convert.Service are listed; unlisted pairs return UNSUPPORTED_UNITS
// rather than inventing new conversion math.
func areaConversions(s *convert.Service) map[string]func(float64) float64 {
	return map[string]func(float64) float64{
		"sqm-sqft": s.SquareMetersToSquareFeet,
		"sqft-sqm": s.SquareFeetToSquareMeters,
		"acre-ha":  s.AcresToHectares,
		"ha-acre":  s.HectaresToAcres,
	}
}

// apiConvertAreaHandler converts {value} from {from} to {to} units using
// the existing bidirectional area-conversion pairs exported by
// convert.Service.
func apiConvertAreaHandler(w http.ResponseWriter, r *http.Request) {
	valueParam := chi.URLParam(r, "value")
	from := strings.ToLower(chi.URLParam(r, "from"))
	to := strings.ToLower(chi.URLParam(r, "to"))

	value, err := strconv.ParseFloat(valueParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "value must be numeric", nil)
		return
	}

	fn, ok := areaConversions(convertService)[from+"-"+to]
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

// dataConversions maps a "from-to" unit pair to the convert.Service
// bidirectional function that implements it. Only pairs actually exported
// by convert.Service are listed; unlisted pairs return UNSUPPORTED_UNITS
// rather than inventing new conversion math.
func dataConversions(s *convert.Service) map[string]func(float64) float64 {
	return map[string]func(float64) float64{
		"b-kb":  s.BytesToKilobytes,
		"kb-b":  s.KilobytesToBytes,
		"kb-mb": s.KilobytesToMegabytes,
		"mb-kb": s.MegabytesToKilobytes,
		"mb-gb": s.MegabytesToGigabytes,
		"gb-mb": s.GigabytesToMegabytes,
		"gb-tb": s.GigabytesToTerabytes,
		"tb-gb": s.TerabytesToGigabytes,
	}
}

// apiConvertDataHandler converts {value} from {from} to {to} units using
// the existing bidirectional data-size-conversion pairs exported by
// convert.Service.
func apiConvertDataHandler(w http.ResponseWriter, r *http.Request) {
	valueParam := chi.URLParam(r, "value")
	from := strings.ToLower(chi.URLParam(r, "from"))
	to := strings.ToLower(chi.URLParam(r, "to"))

	value, err := strconv.ParseFloat(valueParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "value must be numeric", nil)
		return
	}

	fn, ok := dataConversions(convertService)[from+"-"+to]
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

// energyConversions maps a "from-to" unit pair to the convert.Service
// bidirectional function that implements it. Only pairs actually exported
// by convert.Service are listed; unlisted pairs return UNSUPPORTED_UNITS
// rather than inventing new conversion math.
func energyConversions(s *convert.Service) map[string]func(float64) float64 {
	return map[string]func(float64) float64{
		"j-cal": s.JoulesToCalories,
		"cal-j": s.CaloriesToJoules,
		"j-kwh": s.JoulesToKilowattHours,
		"kwh-j": s.KilowattHoursToJoules,
	}
}

// apiConvertEnergyHandler converts {value} from {from} to {to} units using
// the existing bidirectional energy-conversion pairs exported by
// convert.Service.
func apiConvertEnergyHandler(w http.ResponseWriter, r *http.Request) {
	valueParam := chi.URLParam(r, "value")
	from := strings.ToLower(chi.URLParam(r, "from"))
	to := strings.ToLower(chi.URLParam(r, "to"))

	value, err := strconv.ParseFloat(valueParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "value must be numeric", nil)
		return
	}

	fn, ok := energyConversions(convertService)[from+"-"+to]
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

// pressureConversions maps a "from-to" unit pair to the convert.Service
// bidirectional function that implements it. Only pairs actually exported
// by convert.Service are listed; unlisted pairs return UNSUPPORTED_UNITS
// rather than inventing new conversion math.
func pressureConversions(s *convert.Service) map[string]func(float64) float64 {
	return map[string]func(float64) float64{
		"pa-bar": s.PascalsToBar,
		"bar-pa": s.BarToPascals,
		"pa-psi": s.PascalsToPSI,
		"psi-pa": s.PSIToPascals,
		"pa-atm": s.PascalsToAtmospheres,
		"atm-pa": s.AtmospheresToPascals,
	}
}

// apiConvertPressureHandler converts {value} from {from} to {to} units
// using the existing bidirectional pressure-conversion pairs exported by
// convert.Service.
func apiConvertPressureHandler(w http.ResponseWriter, r *http.Request) {
	valueParam := chi.URLParam(r, "value")
	from := strings.ToLower(chi.URLParam(r, "from"))
	to := strings.ToLower(chi.URLParam(r, "to"))

	value, err := strconv.ParseFloat(valueParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "value must be numeric", nil)
		return
	}

	fn, ok := pressureConversions(convertService)[from+"-"+to]
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

// speedConversions maps a "from-to" unit pair to the convert.Service
// bidirectional function that implements it. Only pairs actually exported
// by convert.Service are listed; unlisted pairs return UNSUPPORTED_UNITS
// rather than inventing new conversion math.
func speedConversions(s *convert.Service) map[string]func(float64) float64 {
	return map[string]func(float64) float64{
		"mph-kmh":  s.MphToKmh,
		"kmh-mph":  s.KmhToMph,
		"ms-kmh":   s.MsToKmh,
		"kmh-ms":   s.KmhToMs,
		"knot-kmh": s.KnotsToKmh,
		"kmh-knot": s.KmhToKnots,
	}
}

// apiConvertSpeedHandler converts {value} from {from} to {to} units using
// the existing bidirectional speed-conversion pairs exported by
// convert.Service.
func apiConvertSpeedHandler(w http.ResponseWriter, r *http.Request) {
	valueParam := chi.URLParam(r, "value")
	from := strings.ToLower(chi.URLParam(r, "from"))
	to := strings.ToLower(chi.URLParam(r, "to"))

	value, err := strconv.ParseFloat(valueParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_VALUE", "value must be numeric", nil)
		return
	}

	fn, ok := speedConversions(convertService)[from+"-"+to]
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

// apiConvertColorHandler converts a color value between hex, RGB
// ("r,g,b"), and HSL ("h,s,l") representations using ?value=&from=&to=.
func apiConvertColorHandler(w http.ResponseWriter, r *http.Request) {
	value := r.URL.Query().Get("value")
	from := strings.ToLower(r.URL.Query().Get("from"))
	to := strings.ToLower(r.URL.Query().Get("to"))

	if value == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_VALUE", "value is required", nil)
		return
	}
	if from == "" || to == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_FORMAT", "from and to are required (hex, rgb, hsl)", nil)
		return
	}

	rgb, err := colorToRGB(convertService, value, from)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_COLOR", err.Error(), nil)
		return
	}

	result, err := rgbToColor(convertService, rgb, to)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "UNSUPPORTED_FORMAT", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"value":  value,
		"from":   from,
		"to":     to,
		"result": result,
	})
}

// colorToRGB parses value in the given format ("hex", "rgb", or "hsl")
// into RGB components.
func colorToRGB(s *convert.Service, value, format string) (convert.RGB, error) {
	switch format {
	case "hex":
		return s.HexToRGB(value)
	case "rgb":
		r, g, b, err := parseTriple(value)
		if err != nil {
			return convert.RGB{}, fmt.Errorf("rgb value must be \"r,g,b\": %w", err)
		}
		return convert.RGB{R: int(r), G: int(g), B: int(b)}, nil
	case "hsl":
		h, sVal, l, err := parseTriple(value)
		if err != nil {
			return convert.RGB{}, fmt.Errorf("hsl value must be \"h,s,l\": %w", err)
		}
		return s.HSLToRGB(convert.HSL{H: h, S: sVal, L: l})
	default:
		return convert.RGB{}, fmt.Errorf("unsupported color format: %s (use hex, rgb, or hsl)", format)
	}
}

// rgbToColor formats RGB components into the given output format ("hex",
// "rgb", or "hsl").
func rgbToColor(s *convert.Service, rgb convert.RGB, format string) (string, error) {
	switch format {
	case "hex":
		return s.RGBToHex(rgb)
	case "rgb":
		return fmt.Sprintf("%d,%d,%d", rgb.R, rgb.G, rgb.B), nil
	case "hsl":
		hsl, err := s.RGBToHSL(rgb)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%g,%g,%g", hsl.H, hsl.S, hsl.L), nil
	default:
		return "", fmt.Errorf("unsupported color format: %s (use hex, rgb, or hsl)", format)
	}
}

// parseTriple parses a "a,b,c" comma-separated triple of floats.
func parseTriple(value string) (float64, float64, float64, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("expected 3 comma-separated components, got %d", len(parts))
	}
	nums := make([]float64, 3)
	for i, p := range parts {
		n, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("component %d is not numeric: %w", i+1, err)
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], nil
}

// apiConvertCurrencyHandler converts ?amount= from ?from= to ?to= using
// live ECB reference rates from the free, keyless Frankfurter API.
func apiConvertCurrencyHandler(w http.ResponseWriter, r *http.Request) {
	amountParam := r.URL.Query().Get("amount")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	if from == "" || to == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_CURRENCY", "from and to currency codes are required", nil)
		return
	}

	amount := 1.0
	if amountParam != "" {
		var err error
		amount, err = strconv.ParseFloat(amountParam, 64)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_AMOUNT", "amount must be numeric", nil)
			return
		}
	}

	result, err := convertService.ConvertCurrency(amount, from, to)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "CURRENCY_LOOKUP_FAILED", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiDatetimeFormatHandler formats a Unix timestamp using a named format
// (iso8601, rfc3339, rfc1123, rfc822, kitchen, date, time, datetime) or a
// literal Go reference-time layout, via datetime.FormatDatetime.
func apiDatetimeFormatHandler(w http.ResponseWriter, r *http.Request) {
	timestampParam := chi.URLParam(r, "timestamp")
	format := chi.URLParam(r, "format")

	timestamp, err := strconv.ParseInt(timestampParam, 10, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_TIMESTAMP", "timestamp must be a unix integer", nil)
		return
	}

	result, err := datetime.FormatDatetime(timestamp, format)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_FORMAT", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiDatetimeParseHandler parses a free-form date/time string against a
// list of common layouts via datetime.ParseDateString.
func apiDatetimeParseHandler(w http.ResponseWriter, r *http.Request) {
	value := chi.URLParam(r, "value")

	result, err := datetime.ParseDateString(value)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "UNPARSEABLE_DATE", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiDatetimeCronHandler parses a standard 5-field cron expression and
// returns a breakdown plus next scheduled run times via datetime.ParseCron.
func apiDatetimeCronHandler(w http.ResponseWriter, r *http.Request) {
	expression := r.URL.Query().Get("expression")

	result, err := datetime.ParseCron(expression)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_CRON", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiDatetimeCalendarHandler builds a week-grid calendar for a given
// year/month via datetime.GenerateCalendar.
func apiDatetimeCalendarHandler(w http.ResponseWriter, r *http.Request) {
	yearParam := chi.URLParam(r, "year")
	monthParam := chi.URLParam(r, "month")

	year, err := strconv.Atoi(yearParam)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_YEAR", "year must be an integer", nil)
		return
	}

	month, err := strconv.Atoi(monthParam)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MONTH", "month must be an integer", nil)
		return
	}

	result, err := datetime.GenerateCalendar(year, month)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MONTH", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiDatetimeWorkdaysHandler counts weekdays (Mon-Fri) between two
// YYYY-MM-DD dates inclusive via datetime.WorkdaysBetween.
func apiDatetimeWorkdaysHandler(w http.ResponseWriter, r *http.Request) {
	start := chi.URLParam(r, "start")
	end := chi.URLParam(r, "end")

	result, err := datetime.WorkdaysBetween(start, end)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_DATE", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiDatetimeSunriseHandler computes sunrise/sunset UTC times for a given
// latitude, longitude, and optional YYYY-MM-DD date via
// datetime.SunriseSunset (Almanac for Computers, 1990 algorithm).
func apiDatetimeSunriseHandler(w http.ResponseWriter, r *http.Request) {
	latParam := chi.URLParam(r, "lat")
	lonParam := chi.URLParam(r, "lon")
	date := chi.URLParam(r, "date")

	lat, err := strconv.ParseFloat(latParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_LATITUDE", "lat must be numeric", nil)
		return
	}

	lon, err := strconv.ParseFloat(lonParam, 64)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_LONGITUDE", "lon must be numeric", nil)
		return
	}

	result, err := datetime.SunriseSunset(lat, lon, date)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiDatetimeMoonHandler computes the current lunar phase for an optional
// YYYY-MM-DD date via datetime.MoonPhase (synodic-month method).
func apiDatetimeMoonHandler(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")

	result, err := datetime.MoonPhase(date)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_DATE", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiGenerateQRHandler renders a QR code PNG for the given data, or for a
// Wi-Fi join payload when ssid is supplied instead of data.
func apiGenerateQRHandler(w http.ResponseWriter, r *http.Request) {
	handleQRRequest(w, r)
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

// apiParseEnvHandler parses the raw .env-style document supplied in the
// request body via parseService.ParseEnv.
func apiParseEnvHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_ENV", "request body must contain a .env document", nil)
		return
	}
	parsed, err := parseService.ParseEnv(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_ENV", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, parsed)
}

// apiParseHTMLHandler parses the raw HTML document supplied in the request
// body into a structural summary via parseService.ParseHTML.
func apiParseHTMLHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_HTML", "request body must contain an HTML document", nil)
		return
	}
	parsed, err := parseService.ParseHTML(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_HTML", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, parsed)
}

// apiParseINIHandler parses the raw INI document supplied in the request
// body via parseService.ParseINI.
func apiParseINIHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_INI", "request body must contain an INI document", nil)
		return
	}
	parsed, err := parseService.ParseINI(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_INI", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, parsed)
}

// apiParseLogHandler parses the raw log document supplied in the request
// body, one best-effort entry per line, via parseService.ParseLogLines.
func apiParseLogHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_LOG", "request body must contain log lines", nil)
		return
	}
	parsed, err := parseService.ParseLogLines(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_LOG", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, parsed)
}

// apiParseMarkdownHandler parses the raw Markdown document supplied in the
// request body into a structure summary via
// parseService.ParseMarkdownStructure.
func apiParseMarkdownHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_MARKDOWN", "request body must contain a Markdown document", nil)
		return
	}
	parsed, err := parseService.ParseMarkdownStructure(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MARKDOWN", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, parsed)
}

// apiParseSQLHandler parses the raw SQL statement supplied in the request
// body into a best-effort structure summary via
// parseService.ParseSQLStructure.
func apiParseSQLHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_SQL", "request body must contain a SQL statement", nil)
		return
	}
	parsed, err := parseService.ParseSQLStructure(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_SQL", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, parsed)
}

// apiParseTOMLHandler parses the raw TOML document supplied in the request
// body via parseService.ParseTOML.
func apiParseTOMLHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_TOML", "request body must contain a TOML document", nil)
		return
	}
	parsed, err := parseService.ParseTOML(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_TOML", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, parsed)
}

// apiParseYAMLHandler parses the raw YAML document supplied in the request
// body via parseService.ParseYAML.
func apiParseYAMLHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_YAML", "request body must contain a YAML document", nil)
		return
	}
	parsed, err := parseService.ParseYAML(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_YAML", err.Error(), nil)
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

// apiLanguageDictionaryHandler looks up a word's definitions using the
// free, keyless Free Dictionary API (dictionaryapi.dev).
func apiLanguageDictionaryHandler(w http.ResponseWriter, r *http.Request) {
	word := r.URL.Query().Get("word")
	if strings.TrimSpace(word) == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_WORD", "word query parameter is required", nil)
		return
	}

	result, err := languageService.Dictionary(r.Context(), word)
	if err != nil {
		writeEnvelopeError(w, http.StatusNotFound, "NOT_FOUND", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiLanguageThesaurusHandler looks up a word's synonyms and antonyms
// using the free, keyless Datamuse API.
func apiLanguageThesaurusHandler(w http.ResponseWriter, r *http.Request) {
	word := r.URL.Query().Get("word")
	if strings.TrimSpace(word) == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_WORD", "word query parameter is required", nil)
		return
	}

	result, err := languageService.Thesaurus(r.Context(), word)
	if err != nil {
		writeEnvelopeError(w, http.StatusNotFound, "NOT_FOUND", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiLanguageSpellCheckHandler reports that spell-checking is not
// supported. Not named in IDEA.md's declared Language scope of code/name
// lookup, listing, dictionary, and thesaurus.
func apiLanguageSpellCheckHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "spell-check is not named in this project's declared Language scope of code/name lookup, listing, dictionary, and thesaurus; not supported", nil)
}

// apiLanguageGrammarHandler reports that grammar checking is not supported.
// Not named in IDEA.md's declared Language scope of code/name lookup,
// listing, dictionary, and thesaurus.
func apiLanguageGrammarHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "grammar checking is not named in this project's declared Language scope of code/name lookup, listing, dictionary, and thesaurus; not supported", nil)
}

// apiLanguageTranslateHandler reports that machine translation is not
// supported. IDEA.md explicitly excludes "machine translation" as a
// non-goal and forbids commercial translation among outbound integrations.
func apiLanguageTranslateHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "machine translation is a declared non-goal for this project", nil)
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

// apiLanguageKeywordsHandler returns the most frequent non-stopword words in
// text supplied via ?text= or the raw request body, optionally limited via
// ?limit=.
func apiLanguageKeywordsHandler(w http.ResponseWriter, r *http.Request) {
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

	limit := 10
	if limitParam := r.URL.Query().Get("limit"); limitParam != "" {
		parsed, err := strconv.Atoi(limitParam)
		if err != nil || parsed <= 0 {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limit query parameter must be a positive integer", nil)
			return
		}
		limit = parsed
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"keywords": languageService.Keywords(text, limit),
	})
}

// apiLanguageReadabilityHandler returns Flesch Reading Ease, Flesch-Kincaid
// Grade Level, and Gunning Fog Index scores for text supplied via ?text= or
// the raw request body.
func apiLanguageReadabilityHandler(w http.ResponseWriter, r *http.Request) {
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

	writeEnvelopeOK(w, http.StatusOK, languageService.Readability(text))
}

// apiLanguageReadingTimeHandler estimates reading time for text supplied via
// ?text= or the raw request body, at an optional ?wpm= words-per-minute rate
// (default 200).
func apiLanguageReadingTimeHandler(w http.ResponseWriter, r *http.Request) {
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

	wpm := 0
	if wpmParam := r.URL.Query().Get("wpm"); wpmParam != "" {
		parsed, err := strconv.Atoi(wpmParam)
		if err != nil || parsed <= 0 {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_WPM", "wpm query parameter must be a positive integer", nil)
			return
		}
		wpm = parsed
	}

	wordCount := len(strings.Fields(text))
	writeEnvelopeOK(w, http.StatusOK, languageService.ReadingTime(wordCount, wpm))
}

// apiLanguageSentimentHandler scores text supplied via ?text= or the raw
// request body using a small lexicon-based positive/negative heuristic.
func apiLanguageSentimentHandler(w http.ResponseWriter, r *http.Request) {
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

	writeEnvelopeOK(w, http.StatusOK, languageService.Sentiment(text))
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

// testRequestSpec is the JSON body shape shared by apiTestAPIClientHandler,
// apiTestCurlGeneratorHandler, and apiTestPostmanHandler: a caller-described
// HTTP request to render as generated code/config. These handlers only ever
// format the caller's own input back into a different textual
// representation — they never dial out, matching IDEA.md's outbound-call
// boundary (OSINT and weather tool families only).
type testRequestSpec struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// decodeTestRequestSpec parses and validates the shared testRequestSpec
// body, defaulting Method to GET and rejecting a missing URL.
func decodeTestRequestSpec(r *http.Request) (testRequestSpec, error) {
	var spec testRequestSpec
	if err := decodeJSONBody(r, &spec); err != nil {
		return spec, err
	}
	if strings.TrimSpace(spec.URL) == "" {
		return spec, errMissingURL
	}
	if spec.Method == "" {
		spec.Method = http.MethodGet
	}
	spec.Method = strings.ToUpper(spec.Method)
	return spec, nil
}

// errMissingURL is returned by decodeTestRequestSpec when the caller omits
// the required url field.
var errMissingURL = fmt.Errorf("url is required")

// buildCurlCommand renders a testRequestSpec as a single curl command
// string, following this project's standard curl flag set (-q -LSsf) plus
// -X/-H/-d as needed.
func buildCurlCommand(spec testRequestSpec) string {
	var b strings.Builder
	b.WriteString("curl -q -LSsf")
	if spec.Method != http.MethodGet {
		b.WriteString(" -X ")
		b.WriteString(spec.Method)
	}
	for key, value := range spec.Headers {
		b.WriteString(" -H '")
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteString("'")
	}
	if spec.Body != "" {
		b.WriteString(" -d '")
		b.WriteString(spec.Body)
		b.WriteString("'")
	}
	b.WriteString(" '")
	b.WriteString(spec.URL)
	b.WriteString("'")
	return b.String()
}

// apiTestAPIClientHandler renders a caller-described HTTP request as
// generated client code snippets (curl, JavaScript fetch, Python requests,
// and Go net/http) so a developer can copy working request code for their
// language of choice. Pure string templating of the caller's own input, no
// outbound call.
func apiTestAPIClientHandler(w http.ResponseWriter, r *http.Request) {
	spec, err := decodeTestRequestSpec(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}

	headerLines := make([]string, 0, len(spec.Headers))
	for key, value := range spec.Headers {
		headerLines = append(headerLines, fmt.Sprintf("        %q: %q,", key, value))
	}
	sort.Strings(headerLines)

	jsHeaders := "{}"
	if len(headerLines) > 0 {
		jsHeaders = "{\n" + strings.Join(headerLines, "\n") + "\n    }"
	}
	jsBody := ""
	if spec.Body != "" {
		jsBody = fmt.Sprintf(",\n    body: %q", spec.Body)
	}
	javascript := fmt.Sprintf("fetch(%q, {\n    method: %q,\n    headers: %s%s\n}).then(r => r.json()).then(console.log);", spec.URL, spec.Method, jsHeaders, jsBody)

	pyHeaderLines := make([]string, 0, len(spec.Headers))
	for key, value := range spec.Headers {
		pyHeaderLines = append(pyHeaderLines, fmt.Sprintf("    %q: %q,", key, value))
	}
	sort.Strings(pyHeaderLines)
	pyHeaders := "{}"
	if len(pyHeaderLines) > 0 {
		pyHeaders = "{\n" + strings.Join(pyHeaderLines, "\n") + "\n}"
	}
	pyBody := ""
	if spec.Body != "" {
		pyBody = fmt.Sprintf(", data=%q", spec.Body)
	}
	python := fmt.Sprintf("import requests\n\nresponse = requests.request(%q, %q, headers=%s%s)\nprint(response.json())", spec.Method, spec.URL, pyHeaders, pyBody)

	goHeaderLines := make([]string, 0, len(spec.Headers))
	for key, value := range spec.Headers {
		goHeaderLines = append(goHeaderLines, fmt.Sprintf("\treq.Header.Set(%q, %q)", key, value))
	}
	sort.Strings(goHeaderLines)
	goBody := "nil"
	if spec.Body != "" {
		goBody = fmt.Sprintf("strings.NewReader(%q)", spec.Body)
	}
	golang := fmt.Sprintf("req, _ := http.NewRequest(%q, %q, %s)\n%s\nresp, _ := http.DefaultClient.Do(req)", spec.Method, spec.URL, goBody, strings.Join(goHeaderLines, "\n"))

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"curl":       buildCurlCommand(spec),
		"javascript": javascript,
		"python":     python,
		"go":         golang,
	})
}

// apiTestCurlGeneratorHandler renders a caller-described HTTP request as a
// single curl command string. Pure string formatting of the caller's own
// input, no outbound call.
func apiTestCurlGeneratorHandler(w http.ResponseWriter, r *http.Request) {
	spec, err := decodeTestRequestSpec(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"curl": buildCurlCommand(spec),
	})
}

// postmanCollection is a minimal Postman Collection v2.1 document
// containing the single caller-described request.
type postmanCollection struct {
	Info struct {
		Name   string `json:"name"`
		Schema string `json:"schema"`
	} `json:"info"`
	Item []postmanItem `json:"item"`
}

type postmanItem struct {
	Name    string         `json:"name"`
	Request postmanRequest `json:"request"`
}

type postmanRequest struct {
	Method string          `json:"method"`
	Header []postmanHeader `json:"header"`
	Body   *postmanBody    `json:"body,omitempty"`
	URL    string          `json:"url"`
}

type postmanHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type postmanBody struct {
	Mode string `json:"mode"`
	Raw  string `json:"raw"`
}

// apiTestPostmanHandler renders a caller-described HTTP request as a
// minimal Postman Collection v2.1 JSON document containing that single
// request. Pure JSON templating of the caller's own input, no outbound
// call, no persistence.
func apiTestPostmanHandler(w http.ResponseWriter, r *http.Request) {
	spec, err := decodeTestRequestSpec(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}

	headers := make([]postmanHeader, 0, len(spec.Headers))
	for key, value := range spec.Headers {
		headers = append(headers, postmanHeader{Key: key, Value: value})
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].Key < headers[j].Key })

	var body *postmanBody
	if spec.Body != "" {
		body = &postmanBody{Mode: "raw", Raw: spec.Body}
	}

	collection := postmanCollection{}
	collection.Info.Name = "Generated Request"
	collection.Info.Schema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
	collection.Item = []postmanItem{{
		Name: spec.Method + " " + spec.URL,
		Request: postmanRequest{
			Method: spec.Method,
			Header: headers,
			Body:   body,
			URL:    spec.URL,
		},
	}}

	writeEnvelopeOK(w, http.StatusOK, collection)
}

// apiTestRequestInspectorHandler echoes back the caller's own request:
// method, path, query parameters, headers, and raw body. Directly
// analogous to the already-shipped network.Service.CallerInfo pattern
// (IDEA.md's declared caller/header-inspection scope) — no storage, no
// outbound call.
func apiTestRequestInspectorHandler(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)

	headers := make(map[string]string, len(r.Header))
	for key := range r.Header {
		headers[key] = r.Header.Get(key)
	}

	query := make(map[string]string)
	for key := range r.URL.Query() {
		query[key] = r.URL.Query().Get(key)
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"method":      r.Method,
		"path":        r.URL.Path,
		"query":       query,
		"headers":     headers,
		"body":        string(bodyBytes),
		"remote_addr": r.RemoteAddr,
	})
}

// httpStatusDescriptions supplements net/http.StatusText with a one-line
// human description for the status-codes reference lookup, mirroring
// IDEA.md's declared "HTTP status code ... reference lookup" scope.
var httpStatusDescriptions = map[int]string{
	http.StatusOK:                  "The request succeeded",
	http.StatusCreated:             "The request succeeded and a new resource was created",
	http.StatusAccepted:            "The request has been accepted for processing but is not complete",
	http.StatusNoContent:           "The request succeeded but there is no content to return",
	http.StatusMovedPermanently:    "The resource has permanently moved to a new URL",
	http.StatusFound:               "The resource temporarily resides at a different URL",
	http.StatusNotModified:         "The cached response is still valid",
	http.StatusBadRequest:          "The request was malformed or invalid",
	http.StatusUnauthorized:        "Authentication is required and has failed or not been provided",
	http.StatusForbidden:           "The server understood the request but refuses to authorize it",
	http.StatusNotFound:            "The requested resource could not be found",
	http.StatusMethodNotAllowed:    "The request method is not supported for this resource",
	http.StatusConflict:            "The request conflicts with the current state of the resource",
	http.StatusGone:                "The resource is no longer available and will not be available again",
	http.StatusTooManyRequests:     "The caller has sent too many requests in a given amount of time",
	http.StatusInternalServerError: "The server encountered an unexpected condition",
	http.StatusNotImplemented:      "The server does not support the functionality required to fulfill the request",
	http.StatusBadGateway:          "The server received an invalid response from an upstream server",
	http.StatusServiceUnavailable:  "The server is not ready to handle the request",
	http.StatusGatewayTimeout:      "The upstream server failed to respond in time",
}

// apiTestStatusCodesHandler returns a single HTTP status code's canonical
// reason phrase and description when a {code} path parameter is given, or
// the full reference table when it is omitted. Static lookup data only, no
// outbound call, no persistence.
func apiTestStatusCodesHandler(w http.ResponseWriter, r *http.Request) {
	codeParam := chi.URLParam(r, "code")
	if codeParam == "" {
		table := make(map[string]interface{}, len(httpStatusDescriptions))
		for code := range httpStatusDescriptions {
			table[strconv.Itoa(code)] = map[string]interface{}{
				"text":        http.StatusText(code),
				"description": httpStatusDescriptions[code],
			}
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"codes": table,
		})
		return
	}

	code, err := strconv.Atoi(codeParam)
	if err != nil || http.StatusText(code) == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_CODE", "code must be a known HTTP status code", nil)
		return
	}

	description, ok := httpStatusDescriptions[code]
	if !ok {
		description = ""
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"code":        code,
		"text":        http.StatusText(code),
		"description": description,
	})
}

// apiTestResponseGeneratorHandler generates a mock API response fixture.
// IDEA.md's declared Testing scope names only one mock-API-response
// generator, which already exists as test.Service.GenerateMockAPIResponse
// — this dispatches to it directly rather than inventing a broader
// parameterized (arbitrary status/header/body) response builder that
// IDEA.md does not declare.
func apiTestResponseGeneratorHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeOK(w, http.StatusOK, testService.GenerateMockAPIResponse())
}

// apiTestWebhookHandler accepts a caller-submitted webhook POST and echoes
// back a structured inspection of it (headers, parsed/raw body) in the
// same response cycle. This is a stateless same-request echo, not a
// receive-then-inspect-later store: IDEA.md's non-goals forbid persistent
// storage of user-submitted data, so no payload is retained past this
// request.
func apiTestWebhookHandler(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)

	headers := make(map[string]string, len(r.Header))
	for key := range r.Header {
		headers[key] = r.Header.Get(key)
	}

	var parsedBody interface{}
	jsonValid := false
	if len(bodyBytes) > 0 && json.Unmarshal(bodyBytes, &parsedBody) == nil {
		jsonValid = true
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"method":      r.Method,
		"headers":     headers,
		"raw_body":    string(bodyBytes),
		"parsed_body": parsedBody,
		"json_valid":  jsonValid,
	})
}

// apiTestLoadTestHandler is a permanent gap: a real load-test tool must
// fire real, potentially high-volume outbound HTTP traffic at a
// caller-supplied target URL. IDEA.md restricts outbound calls to only the
// OSINT and weather tool families (a bounded, intentional SSRF surface) —
// deliberately generating load against an arbitrary target is outside that
// boundary and would turn this server into an abuse/DoS proxy.
func apiTestLoadTestHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "load-test is a permanent gap: it would require firing outbound HTTP traffic at a caller-supplied target, outside IDEA.md's outbound-call boundary (OSINT and weather tool families only)", nil)
}

// apiTestMockServerHandler is a permanent gap: a configurable mock HTTP
// server requires either a second runtime-managed listening socket (no
// dynamic-listener lifecycle exists in this codebase, and config-rules.md
// forbids a runtime API for listener/port changes) or persisting
// caller-defined response rules across requests (forbidden by IDEA.md's
// no-persistent-storage-of-user-submitted-data non-goal). Both readings
// hit a declared non-goal.
func apiTestMockServerHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "mock-server is a permanent gap: a configurable dynamic mock server requires either a runtime-managed listening socket or persisted caller-defined response rules, both outside this project's declared scope", nil)
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

// apiOsintSubdomainHandler discovers subdomains of the {domain} path
// parameter by resolving a small fixed wordlist of common subdomain labels
// via the system DNS resolver (osint.SubdomainEnum) — the same trust
// boundary already used by osint.DNSLookup.
func apiOsintSubdomainHandler(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")
	if domain == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_DOMAIN", "domain is required", nil)
		return
	}
	found, err := osintService.SubdomainEnum(domain)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "SUBDOMAIN_ENUM_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"domain":     domain,
		"subdomains": found,
	})
}

// apiOsintTechStackHandler performs a single direct HTTP GET to the ?url=
// query parameter and reports technology signatures observed in the
// response headers/cookies/HTML via osint.TechStack — analogous in shape to
// apiOsintCertHandler's direct TLS handshake, one direct user-directed
// connection only.
func apiOsintTechStackHandler(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("url")
	if strings.TrimSpace(target) == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_URL", "url query parameter is required", nil)
		return
	}
	info, err := osintService.TechStack(target)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadGateway, "TECH_STACK_LOOKUP_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, info)
}

// apiOsintBreachHandler reports that breach-database checking is not
// supported. Free breach-check services (e.g. HaveIBeenPwned) require an
// API key for domain/bulk search, which is outside IDEA.md's declared
// free-and-keyless OSINT trust boundary (IDEA.md line 34: OSINT is scoped
// to IP geolocation, WHOIS, DNS, and TLS certificate inspection only).
func apiOsintBreachHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "breach-database checking requires a keyed third-party API, outside this project's declared free/keyless OSINT trust boundary; not supported", nil)
}

// apiOsintCompanyHandler reports that company-data lookup is not supported.
// Company enrichment (e.g. Clearbit-style lookups) is a commercial, keyed
// service — outside IDEA.md's declared free-and-keyless OSINT scope
// (IP geolocation, WHOIS, DNS, TLS cert only) and forbidden by the
// non-goals list's ban on paid/keyed third-party APIs.
func apiOsintCompanyHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "company-data lookup requires a commercial keyed API, outside this project's declared free/keyless OSINT trust boundary; not supported", nil)
}

// apiOsintMetadataHandler reports that generic file-metadata extraction is
// not supported as an OSINT tool. IDEA.md scopes OSINT to IP geolocation,
// WHOIS, DNS, and TLS certificate inspection only — file-metadata
// extraction is not one of those mechanisms, and image/document metadata
// extraction already exists as its own tool (apiImageMetadataHandler);
// duplicating it here would invent behavior IDEA.md does not scope to OSINT.
func apiOsintMetadataHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "generic file-metadata extraction is outside this project's declared OSINT scope (IP geolocation, WHOIS, DNS, TLS cert only); see the image/metadata tool for file-metadata inspection", nil)
}

// apiOsintPhoneHandler reports that phone-number intelligence lookup is not
// supported. Carrier/line-type/reputation lookup services are commercial
// and keyed — outside IDEA.md's declared free-and-keyless OSINT scope; basic
// phone-number format validation is already covered by validate/phone.
func apiOsintPhoneHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "phone-number intelligence requires a commercial keyed API, outside this project's declared free/keyless OSINT trust boundary; see validate/phone for format validation", nil)
}

// apiOsintSocialHandler reports that social-media profile discovery is not
// supported. Finding a person's social profiles means firing off requests
// to dozens of third-party platforms — a much larger and fundamentally
// different outbound surface than IDEA.md's four narrowly-scoped OSINT
// mechanisms (IP geolocation, WHOIS, DNS, TLS cert), and not user-directed
// to a single target the caller names.
func apiOsintSocialHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "social-media profile discovery requires probing many third-party platforms, outside this project's declared OSINT trust boundary; not supported", nil)
}

// apiOsintUsernameHandler reports that cross-platform username enumeration
// is not supported, for the same reason as apiOsintSocialHandler: checking
// a username against dozens of third-party sites is a much larger and
// different outbound surface than IDEA.md's four declared OSINT mechanisms.
func apiOsintUsernameHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "cross-platform username enumeration requires probing many third-party sites, outside this project's declared OSINT trust boundary; not supported", nil)
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

// apiResearchArxivHandler looks up an arXiv paper by ID (JSON body
// {"id":"..."} or ?id= query parameter) using the free, keyless arXiv
// query API.
func apiResearchArxivHandler(w http.ResponseWriter, r *http.Request) {
	id := queryOrJSONField(r, "id")
	if id == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_ID", "id is required (JSON body or ?id= query parameter)", nil)
		return
	}

	result, err := researchService.ArxivLookup(r.Context(), id)
	if err != nil {
		writeEnvelopeError(w, http.StatusNotFound, "NOT_FOUND", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiResearchBibtexHandler reports that BibTeX parsing/formatting is not
// supported. IDEA.md's declared Research scope covers only citation
// formatting (APA/MLA/Chicago), bibliography generation, and DOI
// formatting/validation — BibTeX is a distinct format not named in scope.
func apiResearchBibtexHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "BibTeX parsing/formatting is outside this project's declared Research scope (citation formatting, bibliography generation, DOI only); not supported", nil)
}

// apiResearchFootnotesHandler reports that footnote/endnote formatting is
// not supported. Not named in IDEA.md's declared Research scope.
func apiResearchFootnotesHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "footnote/endnote formatting is outside this project's declared Research scope (citation formatting, bibliography generation, DOI only); not supported", nil)
}

// apiResearchIsbnHandler looks up a book's metadata by ISBN (JSON body
// {"isbn":"..."} or ?isbn= query parameter) using the free, keyless Open
// Library Books API.
func apiResearchIsbnHandler(w http.ResponseWriter, r *http.Request) {
	isbn := queryOrJSONField(r, "isbn")
	if isbn == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_ISBN", "isbn is required (JSON body or ?isbn= query parameter)", nil)
		return
	}

	result, err := researchService.ISBNLookup(r.Context(), isbn)
	if err != nil {
		writeEnvelopeError(w, http.StatusNotFound, "NOT_FOUND", err.Error(), nil)
		return
	}

	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiResearchMetadataHandler reports that web-page metadata extraction is
// not supported. Not named in IDEA.md's declared Research scope, and
// fetching an arbitrary caller-supplied URL (rather than querying a single
// fixed, trusted provider) is a broader SSRF surface than this project
// takes on.
func apiResearchMetadataHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "web-page metadata extraction is outside this project's declared Research scope and would require fetching arbitrary caller-supplied URLs rather than querying a single fixed, trusted provider; not supported", nil)
}

// apiResearchOutlineHandler reports that document outline generation is not
// supported. Not named in IDEA.md's declared Research scope; heading
// extraction from Markdown is already covered by parse/markdown.
func apiResearchOutlineHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "document outline generation is outside this project's declared Research scope; see parse/markdown for heading extraction from Markdown", nil)
}

// apiResearchPdfExtractHandler reports that PDF text extraction is not
// supported. It would require a new third-party PDF-parsing dependency,
// and is not named in IDEA.md's declared Research scope.
func apiResearchPdfExtractHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "PDF text extraction requires a new third-party dependency and is outside this project's declared Research scope; not supported", nil)
}

// apiResearchReadabilityHandler reports that reader-mode article extraction
// is not supported. Not named in IDEA.md's declared Research scope, and
// fetching an arbitrary caller-supplied URL (rather than querying a single
// fixed, trusted provider) is a broader SSRF surface than this project
// takes on.
func apiResearchReadabilityHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "reader-mode article extraction is outside this project's declared Research scope and would require fetching arbitrary caller-supplied URLs rather than querying a single fixed, trusted provider; not supported", nil)
}

// apiResearchScraperHandler reports that general web scraping is not
// supported. It would require fetching arbitrary caller-supplied URLs, a
// broader SSRF surface than the narrow, mitigated outbound-call mechanisms
// (fixed, trusted providers) this project uses elsewhere.
func apiResearchScraperHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "general-purpose web scraping requires fetching arbitrary caller-supplied URLs rather than querying a single fixed, trusted provider; not supported", nil)
}

// apiResearchSummarizeHandler reports that text summarization is not
// supported. A genuine summarizer needs an external/keyed NLP or LLM
// service, which IDEA.md excludes ("every outbound integration must be
// free and keyless"); it is not named in IDEA.md's declared Research scope.
func apiResearchSummarizeHandler(w http.ResponseWriter, r *http.Request) {
	writeEnvelopeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "text summarization would require an external or keyed NLP/LLM service, which this project's free/keyless integration policy excludes; not supported", nil)
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

// apiFunDadJokeHandler returns a single random dad joke from the curated
// built-in list in funService.DadJoke.
func apiFunDadJokeHandler(w http.ResponseWriter, r *http.Request) {
	text, err := funService.DadJoke()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "DAD_JOKE_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"text": text})
}

// apiFunProgrammingJokeHandler returns a single random programming joke from
// the curated built-in list in funService.ProgrammingJoke.
func apiFunProgrammingJokeHandler(w http.ResponseWriter, r *http.Request) {
	text, err := funService.ProgrammingJoke()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "PROGRAMMING_JOKE_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"text": text})
}

// apiFunQuoteHandler returns a single random inspirational quote from the
// curated built-in list in funService.Quote.
func apiFunQuoteHandler(w http.ResponseWriter, r *http.Request) {
	text, err := funService.Quote()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "QUOTE_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"text": text})
}

// apiFunFactHandler returns a single random fact from the curated built-in
// list in funService.Fact.
func apiFunFactHandler(w http.ResponseWriter, r *http.Request) {
	text, err := funService.Fact()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "FACT_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"text": text})
}

// apiFunRiddleHandler returns a single random riddle question/answer pair
// from the curated built-in list in funService.Riddle.
func apiFunRiddleHandler(w http.ResponseWriter, r *http.Request) {
	riddle, err := funService.Riddle()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "RIDDLE_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, riddle)
}

// apiFunTriviaHandler returns a single random trivia question/answer pair
// from the curated built-in list in funService.Trivia.
func apiFunTriviaHandler(w http.ResponseWriter, r *http.Request) {
	fact, err := funService.Trivia()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "TRIVIA_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, fact)
}

// apiFunMotivationalHandler returns a single random motivational quote from
// the curated built-in list in funService.Motivational.
func apiFunMotivationalHandler(w http.ResponseWriter, r *http.Request) {
	text, err := funService.Motivational()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "MOTIVATIONAL_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"text": text})
}

// apiFunInsultHandler returns a single random playful mock-insult from the
// curated built-in list in funService.Insult.
func apiFunInsultHandler(w http.ResponseWriter, r *http.Request) {
	text, err := funService.Insult()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "INSULT_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"text": text})
}

// apiFunComplimentHandler returns a single random compliment from the
// curated built-in list in funService.Compliment.
func apiFunComplimentHandler(w http.ResponseWriter, r *http.Request) {
	text, err := funService.Compliment()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "COMPLIMENT_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"text": text})
}

// apiFunMemeHandler returns a single random text-only meme caption from the
// curated built-in list in funService.Meme (no image generation).
func apiFunMemeHandler(w http.ResponseWriter, r *http.Request) {
	text, err := funService.Meme()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "MEME_GENERATION_FAILED", err.Error(), nil)
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

// apiDevCronHandler parses a 5-field cron expression, reusing the same
// datetime.ParseCron helper as apiDatetimeCronHandler.
func apiDevCronHandler(w http.ResponseWriter, r *http.Request) {
	expression := r.URL.Query().Get("expression")
	result, err := datetime.ParseCron(expression)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_CRON", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, result)
}

// apiDevJWTHandler decodes (never verifies) the header and payload of a
// JSON Web Token, reusing the same decodeJWTSegment helper as
// apiParseJWTHandler and apiCryptoJWTDecodeHandler. No signature
// verification is performed — this is a read-only debug/inspection tool.
func apiDevJWTHandler(w http.ResponseWriter, r *http.Request) {
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

// apiDevEchoHandler reflects the caller's own request details back as
// JSON: method, path, query parameters, headers, remote address, and raw
// body. This is a genuinely new debug tool with no service dependency.
func apiDevEchoHandler(w http.ResponseWriter, r *http.Request) {
	body, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}

	headers := make(map[string]string, len(r.Header))
	for name, values := range r.Header {
		headers[name] = strings.Join(values, ", ")
	}

	query := make(map[string]string, len(r.URL.Query()))
	for name, values := range r.URL.Query() {
		query[name] = strings.Join(values, ", ")
	}

	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"method":      r.Method,
		"path":        r.URL.Path,
		"query":       query,
		"headers":     headers,
		"remote_addr": r.RemoteAddr,
		"body":        string(body),
	})
}

// apiDevFormatCSSHandler formats (or, with ?minify=true, minifies) the raw
// CSS document supplied in the request body.
func apiDevFormatCSSHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if config.IsTruthy(r.URL.Query().Get("minify")) {
		writeEnvelopeOK(w, http.StatusOK, map[string]string{"formatted": devService.MinifyCSS(string(raw))})
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"formatted": devService.FormatCSS(string(raw))})
}

// apiDevFormatHTMLHandler formats (or, with ?minify=true, minifies) the raw
// HTML document supplied in the request body.
func apiDevFormatHTMLHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if config.IsTruthy(r.URL.Query().Get("minify")) {
		writeEnvelopeOK(w, http.StatusOK, map[string]string{"formatted": devService.MinifyHTML(string(raw))})
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"formatted": devService.FormatHTML(string(raw))})
}

// apiDevFormatJSHandler formats (or, with ?minify=true, minifies) the raw
// JavaScript source supplied in the request body.
func apiDevFormatJSHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if config.IsTruthy(r.URL.Query().Get("minify")) {
		writeEnvelopeOK(w, http.StatusOK, map[string]string{"formatted": devService.MinifyJS(string(raw))})
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"formatted": devService.FormatJS(string(raw))})
}

// apiDevFormatSQLHandler formats the raw SQL query supplied in the request
// body by breaking it onto multiple lines by clause keyword. There is no
// minify variant — a formatted SQL query is the tool's only mode.
func apiDevFormatSQLHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{"formatted": devService.FormatSQL(string(raw))})
}

// apiDevFormatXMLHandler formats (or, with ?minify=true, minifies) the raw
// XML document supplied in the request body.
func apiDevFormatXMLHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := readRequestBody(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if config.IsTruthy(r.URL.Query().Get("minify")) {
		formatted, err := devService.MinifyXML(string(raw))
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_XML", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]string{"formatted": formatted})
		return
	}
	formatted, err := devService.FormatXML(string(raw))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_XML", err.Error(), nil)
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

// apiImageAvatarHandler renders an initials-based avatar as a PNG image
// under the image/ tool category. It is a thin wrapper around the same
// generateService.Avatar used by apiGenerateAvatarHandler — no duplicated
// logic.
func apiImageAvatarHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	initials := q.Get("initials")
	if initials == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_INITIALS", "initials is required", nil)
		return
	}
	size, err := strconv.Atoi(q.Get("size"))
	if err != nil || size <= 0 {
		size = 128
	}

	png, err := generateService.Avatar(initials, size)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "AVATAR_GENERATION_FAILED", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	w.Write(png)
}

// apiImageBarcodeHandler renders a 1D barcode (ean13, upca, code128, or
// code39) as a PNG image under the image/ tool category. It is a thin
// wrapper around the same generateService.Barcode used by
// apiGenerateBarcodeHandler — no duplicated logic.
func apiImageBarcodeHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := q.Get("format")
	data := q.Get("data")
	if data == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_DATA", "data is required", nil)
		return
	}
	width, err := strconv.Atoi(q.Get("width"))
	if err != nil || width <= 0 {
		width = 300
	}
	height, err := strconv.Atoi(q.Get("height"))
	if err != nil || height <= 0 {
		height = 100
	}

	png, err := generateService.Barcode(format, data, width, height)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "BARCODE_GENERATION_FAILED", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	w.Write(png)
}

// apiImageIdenticonHandler renders a deterministic sha256-derived
// identicon as a PNG image under the image/ tool category. It is a thin
// wrapper around the same generateService.Identicon used by
// apiGenerateIdenticonHandler — no duplicated logic.
func apiImageIdenticonHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	seed := q.Get("seed")
	if seed == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_SEED", "seed is required", nil)
		return
	}
	size, err := strconv.Atoi(q.Get("size"))
	if err != nil || size <= 0 {
		size = 256
	}

	png, err := generateService.Identicon(seed, size)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IDENTICON_GENERATION_FAILED", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	w.Write(png)
}

// apiImageQRHandler renders a QR code PNG for the given data, or for a
// Wi-Fi join payload when ssid is supplied instead of data. This mirrors
// apiGenerateQRHandler exactly; it exists only so the /api/v1/image/qr
// path works the same as /api/v1/generate/qr.
func apiImageQRHandler(w http.ResponseWriter, r *http.Request) {
	handleQRRequest(w, r)
}

// handleQRRequest implements the shared body of apiGenerateQRHandler and
// apiImageQRHandler: either ?data=... is encoded directly, or ?ssid=...
// (with optional password/security/hidden) is built into a standard
// Wi-Fi QR join payload per IDEA.md's Wi-Fi QR code requirement.
func handleQRRequest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	data := q.Get("data")
	ssid := q.Get("ssid")

	var content string
	switch {
	case ssid != "":
		payload, err := generate.BuildWifiQRPayload(ssid, q.Get("password"), q.Get("security"), config.IsTruthy(q.Get("hidden")))
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_WIFI_QR_PARAMS", err.Error(), nil)
			return
		}
		content = payload
	case data != "":
		content = data
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_DATA", "data (or ssid for a Wi-Fi QR code) is required", nil)
		return
	}

	width, err := strconv.Atoi(q.Get("width"))
	if err != nil || width <= 0 {
		width = 300
	}
	height, err := strconv.Atoi(q.Get("height"))
	if err != nil || height <= 0 {
		height = 300
	}

	png, err := generateService.QR(content, width, height)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "QR_GENERATION_FAILED", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	w.Write(png)
}

// apiImageFilterHandler decodes an uploaded image and applies a named
// filter (grayscale, sepia, invert, blur, brighten, darken) to it.
func apiImageFilterHandler(w http.ResponseWriter, r *http.Request) {
	data, err := readUploadedImage(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_READ_FAILED", err.Error(), nil)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_FILTER", "name is required", nil)
		return
	}
	amount, err := strconv.ParseFloat(r.FormValue("amount"), 64)
	if err != nil {
		amount = 0
	}

	svc := image.New()
	if err := svc.Load(data); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_DECODE_FAILED", err.Error(), nil)
		return
	}
	if err := svc.ApplyFilter(name, amount); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_FILTER_FAILED", err.Error(), nil)
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

// apiImageOptimizeHandler decodes an uploaded image and re-encodes it in
// the requested format, reporting the original and optimized byte sizes
// via response headers. quality only affects JPEG output — Go's standard
// library has no lossy quality knob for PNG or GIF, so those always use
// the encoder's fixed lossless compression; this endpoint does not invent
// a fake compression ratio for them.
func apiImageOptimizeHandler(w http.ResponseWriter, r *http.Request) {
	data, err := readUploadedImage(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_READ_FAILED", err.Error(), nil)
		return
	}

	quality, err := strconv.Atoi(r.FormValue("quality"))
	if err != nil || quality <= 0 {
		quality = 75
	}

	svc := image.New()
	if err := svc.Load(data); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_DECODE_FAILED", err.Error(), nil)
		return
	}

	format := r.FormValue("format")
	out, err := svc.Optimize(orDefaultOutputFormat(format), quality)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_OPTIMIZE_FAILED", err.Error(), nil)
		return
	}

	w.Header().Set("X-Original-Size", strconv.Itoa(len(data)))
	w.Header().Set("X-Optimized-Size", strconv.Itoa(len(out)))
	w.Header().Set("Content-Type", imageContentType(format))
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}

// apiImageWatermarkHandler decodes an uploaded image and tiles the given
// text across it as a semi-transparent watermark.
func apiImageWatermarkHandler(w http.ResponseWriter, r *http.Request) {
	data, err := readUploadedImage(r)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_READ_FAILED", err.Error(), nil)
		return
	}

	text := r.FormValue("text")
	if text == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_TEXT", "text is required", nil)
		return
	}
	opacity, err := strconv.ParseFloat(r.FormValue("opacity"), 64)
	if err != nil {
		opacity = 0
	}

	svc := image.New()
	if err := svc.Load(data); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_DECODE_FAILED", err.Error(), nil)
		return
	}
	if err := svc.Watermark(text, opacity); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IMAGE_WATERMARK_FAILED", err.Error(), nil)
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

// cryptoCertificateRequest is the JSON body shape accepted by
// apiCryptoCertificateHandler.
type cryptoCertificateRequest struct {
	Mode        string `json:"mode"`
	CommonName  string `json:"common_name"`
	ValidDays   int    `json:"valid_days"`
	Certificate string `json:"certificate"`
}

// apiCryptoCertificateHandler handles self-signed X.509 certificate
// generation and PEM certificate parsing in one endpoint, selected by Mode.
// Composes crypto.GenerateCertificate and crypto.ParseCertificate.
func apiCryptoCertificateHandler(w http.ResponseWriter, r *http.Request) {
	var body cryptoCertificateRequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if body.Mode == "" {
		body.Mode = "generate"
	}

	switch strings.ToLower(body.Mode) {
	case "generate":
		if body.CommonName == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_COMMON_NAME", "common_name is required", nil)
			return
		}
		certPEM, keyPEM, err := crypto.GenerateCertificate(body.CommonName, body.ValidDays)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "GENERATE_FAILED", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"mode":        body.Mode,
			"certificate": certPEM,
			"private_key": keyPEM,
		})
	case "parse":
		if body.Certificate == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_CERTIFICATE", "certificate is required", nil)
			return
		}
		details, err := crypto.ParseCertificate(body.Certificate)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "PARSE_FAILED", err.Error(), nil)
			return
		}
		details["mode"] = body.Mode
		writeEnvelopeOK(w, http.StatusOK, details)
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MODE", "mode must be generate or parse", nil)
	}
}

// cryptoEd25519Request is the JSON body shape accepted by
// apiCryptoEd25519Handler.
type cryptoEd25519Request struct {
	Mode       string `json:"mode"`
	Message    string `json:"message"`
	Signature  string `json:"signature"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

// apiCryptoEd25519Handler handles Ed25519 keypair generation, signing, and
// signature verification in one endpoint, selected by Mode. Composes
// crypto.GenerateEd25519Keys, crypto.Ed25519Sign, and crypto.Ed25519Verify.
func apiCryptoEd25519Handler(w http.ResponseWriter, r *http.Request) {
	var body cryptoEd25519Request
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if body.Mode == "" {
		body.Mode = "generate"
	}

	switch strings.ToLower(body.Mode) {
	case "generate":
		privateKey, publicKey, err := crypto.GenerateEd25519Keys()
		if err != nil {
			writeEnvelopeError(w, http.StatusInternalServerError, "GENERATE_FAILED", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"mode":        body.Mode,
			"private_key": privateKey,
			"public_key":  publicKey,
		})
	case "sign":
		if body.Message == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_MESSAGE", "message is required", nil)
			return
		}
		if body.PrivateKey == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_PRIVATE_KEY", "private_key is required", nil)
			return
		}
		signature, err := crypto.Ed25519Sign(body.Message, body.PrivateKey)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "SIGN_FAILED", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"mode":      body.Mode,
			"signature": signature,
		})
	case "verify":
		if body.Message == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_MESSAGE", "message is required", nil)
			return
		}
		if body.Signature == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_SIGNATURE", "signature is required", nil)
			return
		}
		if body.PublicKey == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_PUBLIC_KEY", "public_key is required", nil)
			return
		}
		valid, err := crypto.Ed25519Verify(body.Message, body.Signature, body.PublicKey)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "VERIFY_FAILED", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"mode":  body.Mode,
			"valid": valid,
		})
	default:
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_MODE", "mode must be generate, sign, or verify", nil)
	}
}

// cryptoPGPRequest is the JSON body shape accepted by apiCryptoPGPHandler.
type cryptoPGPRequest struct {
	Mode       string `json:"mode"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Plaintext  string `json:"plaintext"`
	Ciphertext string `json:"ciphertext"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

// apiCryptoPGPHandler handles PGP keypair generation, encryption, and
// decryption in one endpoint, selected by Mode. Composes
// crypto.GeneratePGPKeys, crypto.PGPEncrypt, and crypto.PGPDecrypt.
func apiCryptoPGPHandler(w http.ResponseWriter, r *http.Request) {
	var body cryptoPGPRequest
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}
	if body.Mode == "" {
		body.Mode = "generate"
	}

	switch strings.ToLower(body.Mode) {
	case "generate":
		if body.Name == "" || body.Email == "" {
			writeEnvelopeError(w, http.StatusBadRequest, "MISSING_IDENTITY", "name and email are required", nil)
			return
		}
		publicKey, privateKey, err := crypto.GeneratePGPKeys(body.Name, body.Email)
		if err != nil {
			writeEnvelopeError(w, http.StatusBadRequest, "GENERATE_FAILED", err.Error(), nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
			"mode":        body.Mode,
			"public_key":  publicKey,
			"private_key": privateKey,
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
		ciphertext, err := crypto.PGPEncrypt(body.Plaintext, body.PublicKey)
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
		plaintext, err := crypto.PGPDecrypt(body.Ciphertext, body.PrivateKey)
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

// apiGenerateBarcodeHandler renders a 1D barcode (ean13, upca, code128, or
// code39) as a PNG image using generateService.Barcode.
func apiGenerateBarcodeHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := q.Get("format")
	data := q.Get("data")
	if data == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_DATA", "data is required", nil)
		return
	}
	width, err := strconv.Atoi(q.Get("width"))
	if err != nil || width <= 0 {
		width = 300
	}
	height, err := strconv.Atoi(q.Get("height"))
	if err != nil || height <= 0 {
		height = 100
	}

	png, err := generateService.Barcode(format, data, width, height)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "BARCODE_GENERATION_FAILED", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	w.Write(png)
}

// apiGenerateAvatarHandler renders an initials-based avatar as a PNG image
// using generateService.Avatar.
func apiGenerateAvatarHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	initials := q.Get("initials")
	if initials == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_INITIALS", "initials is required", nil)
		return
	}
	size, err := strconv.Atoi(q.Get("size"))
	if err != nil || size <= 0 {
		size = 128
	}

	png, err := generateService.Avatar(initials, size)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "AVATAR_GENERATION_FAILED", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	w.Write(png)
}

// apiGenerateIdenticonHandler renders a deterministic sha256-derived
// identicon as a PNG image using generateService.Identicon.
func apiGenerateIdenticonHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	seed := q.Get("seed")
	if seed == "" {
		writeEnvelopeError(w, http.StatusBadRequest, "MISSING_SEED", "seed is required", nil)
		return
	}
	size, err := strconv.Atoi(q.Get("size"))
	if err != nil || size <= 0 {
		size = 256
	}

	png, err := generateService.Identicon(seed, size)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "IDENTICON_GENERATION_FAILED", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	w.Write(png)
}

// apiGenerateDockerfileHandler renders a minimal idiomatic multi-stage
// Dockerfile for the requested language using generateService.Dockerfile.
func apiGenerateDockerfileHandler(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	dockerfile, err := generateService.Dockerfile(lang)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "DOCKERFILE_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"dockerfile": dockerfile,
	})
}

// apiGenerateGitignoreHandler renders a union of curated .gitignore
// boilerplate for the comma-separated languages/tools in ?lang= using
// generateService.Gitignore.
func apiGenerateGitignoreHandler(w http.ResponseWriter, r *http.Request) {
	langs := r.URL.Query().Get("lang")
	gitignore, err := generateService.Gitignore(langs)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "GITIGNORE_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"gitignore": gitignore,
	})
}

// apiGenerateLicenseHandler renders canonical license text for ?type=
// (mit, apache-2.0, gpl-3.0, bsd-3-clause, isc), optionally substituting
// ?author=/?year=, using generateService.License.
func apiGenerateLicenseHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	licenseType := q.Get("type")
	author := q.Get("author")
	year := q.Get("year")

	license, err := generateService.License(licenseType, author, year)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "LICENSE_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"license": license,
	})
}

// apiGenerateConfigHandler renders a configuration-file scaffold in the
// requested format (yaml, json, env, toml) from the request's query
// parameters (excluding "format") as key=value pairs, using
// generateService.Config.
func apiGenerateConfigHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := q.Get("format")

	values := make(map[string]string)
	for key, vals := range q {
		if key == "format" || len(vals) == 0 {
			continue
		}
		values[key] = vals[0]
	}

	config, err := generateService.Config(format, values)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "CONFIG_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"config": config,
	})
}

// apiGenerateSQLHandler decodes a JSON {"table": "...", "columns": [...]}
// body and returns the generated CREATE TABLE statement, using
// generateService.SQL. Scope is limited to CREATE TABLE generation only.
func apiGenerateSQLHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Table   string               `json:"table"`
		Columns []generate.SQLColumn `json:"columns"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "INVALID_BODY", err.Error(), nil)
		return
	}

	sql, err := generateService.SQL(body.Table, body.Columns)
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "SQL_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"sql": sql,
	})
}

// apiGenerateSSHKeyHandler generates a stateless Ed25519 SSH key pair and
// returns both keys in the JSON envelope; nothing is persisted, so the
// private key is returned in full (this is the only time it is available).
func apiGenerateSSHKeyHandler(w http.ResponseWriter, r *http.Request) {
	keyPair, err := generateService.SSHKey()
	if err != nil {
		writeEnvelopeError(w, http.StatusInternalServerError, "SSH_KEY_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"private_key": keyPair.PrivateKey,
		"public_key":  keyPair.PublicKey,
	})
}

// requestBaseURL builds a request-derived base URL ("scheme://host") using
// the same TLS/X-Forwarded-Proto detection as securityHeadersMiddleware,
// since api_utils.go handlers have no *config.Config to call getBaseURL with.
func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

// apiGenerateAPIDocsHandler renders API documentation for ?format=
// (markdown, default, or json — which duplicates /openapi.json's structure)
// and ?version=, using generateService.APIDocs against the shared
// swagger.GenerateSpec output.
func apiGenerateAPIDocsHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := q.Get("format")
	version := q.Get("version")
	if version == "" {
		version = "v1"
	}

	docs, err := generateService.APIDocs(format, version, requestBaseURL(r))
	if err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "API_DOCS_GENERATION_FAILED", err.Error(), nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"docs": docs,
	})
}

// apiGeneratePlaceholderHandler generates a placeholder image of
// {width}x{height} under the generate/ tool category. It is a thin wrapper
// around the same imageService.GeneratePlaceholder used by
// apiImagePlaceholderHandler — no duplicated logic.
func apiGeneratePlaceholderHandler(w http.ResponseWriter, r *http.Request) {
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

	w.Header().Set("Content-Type", imageContentType(format))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
