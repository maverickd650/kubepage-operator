package dashboard

// Field labels shared by more than one widget implementation in this
// package (internal/dashboard/prometheus.go has the Status/Targets-family
// labels; these are the rest), pulled into constants so goconst doesn't
// flag every widget re-typing the same string.
const (
	labelVersion     = "Version"
	labelStreams     = "Streams"
	labelRecipes     = "Recipes"
	labelLinks       = "Links"
	labelCollections = "Collections"
	labelScenes      = "Scenes"
	labelImages      = "Images"
	labelGalleries   = "Galleries"
	labelUptime      = "Uptime"
	labelValue       = "Value"
	labelTunnel      = "Tunnel"
	labelClients     = "Clients"
	labelWeather     = "Weather"
	labelConditions  = "Conditions"
	labelCPU         = "CPU"
	labelMemory      = "Memory"
	labelStorage     = "Storage"
	labelSeries      = "Series"
	labelMovies      = "Movies"
	labelQueue       = "Queue"
	labelQueries     = "Queries"
	labelUp          = "Up"

	// headerXAPIKey is the "X-Api-Key" request header several *arr-family
	// and Docker-management widgets (sonarr.go, radarr.go, jellyseerr.go,
	// portainer.go) use for their static API key auth — pulled out here so
	// goconst doesn't flag the repeated literal across those files.
	headerXAPIKey = "X-Api-Key"

	// secretAPIKey is the Secrets map key ("apiKey") shared by every widget
	// whose auth is a static API key sent as a header rather than one with
	// its own established secret-field convention (sonarr.go, radarr.go,
	// jellyseerr.go, immich.go, portainer.go). openweathermap.go predates
	// this constant and keeps its own identically-valued
	// openWeatherMapSecretAPIKey rather than being migrated for a
	// cross-widget rename.
	secretAPIKey = "apiKey"

	// secretPassword is the Secrets map key ("password") shared by the two
	// widgets using a plain password rather than a token/API-key (adguard.go's
	// HTTP Basic auth, pihole.go's session login) — deliberately not used for
	// their JSON-RPC/wire struct tags of the same name, which must stay
	// literal Go struct tags.
	secretPassword = "password"

	// secretUsername is the Secrets map key ("username") adguard.go's HTTP
	// Basic auth uses alongside secretPassword.
	secretUsername = "username"

	// unitsImperial is the "units" config value openmeteo.go and
	// openweathermap.go both switch on for Fahrenheit output (their default
	// is metric/Celsius).
	unitsImperial = "imperial"
)

// weatherLabelAndSuffix resolves a weather widget's display label (falling
// back to labelWeather when unset) and temperature suffix (°C, or °F for
// unitsImperial) from its config — the one piece of config-decode logic
// openmeteo.go and openweathermap.go's Poll and Sample all four share, kept
// here so they can't drift out of sync with each other.
func weatherLabelAndSuffix(label, units string) (resolvedLabel, tempSuffix string) {
	if label == "" {
		label = labelWeather
	}
	tempSuffix = "°C"
	if units == unitsImperial {
		tempSuffix = "°F"
	}
	return label, tempSuffix
}
