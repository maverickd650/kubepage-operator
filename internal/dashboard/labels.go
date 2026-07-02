package dashboard

// Field labels shared by more than one widget implementation in this
// package (internal/dashboard/prometheus.go has the Status/Targets-family
// labels; these are the rest), pulled into constants so goconst doesn't
// flag every widget re-typing the same string.
const (
	labelVersion    = "Version"
	labelStreams    = "Streams"
	labelRecipes    = "Recipes"
	labelLinks      = "Links"
	labelScenes     = "Scenes"
	labelImages     = "Images"
	labelGalleries  = "Galleries"
	labelUptime     = "Uptime"
	labelValue      = "Value"
	labelTunnel     = "Tunnel"
	labelClients    = "Clients"
	labelWeather    = "Weather"
	labelConditions = "Conditions"
	labelCPU        = "CPU"
	labelMemory     = "Memory"
	labelStorage    = "Storage"

	// unitsImperial is the "units" config value openmeteo.go and
	// openweathermap.go both switch on for Fahrenheit output (their default
	// is metric/Celsius).
	unitsImperial = "imperial"
)
