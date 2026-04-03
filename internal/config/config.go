package config

import (
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	Server struct {
		Port    string
		Host    string
		Profile string
	}
	Database struct {
		Host     string
		Port     string
		User     string
		Password string
		Name     string
	}
	MQTT struct {
		Broker          string
		Host            string
		Port            string
		ClientID        string
		Username        string
		Password        string
		PublishTimeout  time.Duration
		PublishRetries  int
		PublishRetryDelay time.Duration
	}
	Frontend struct {
		AllowedOrigins string
	}
	Session struct {
		CookieName string
		Domain     string
		Secure     bool
		SameSite   string
	}
	Valkey struct {
		Addr     string
		Password string
	}
	OAuth struct {
		ClientID     string
		ClientSecret string
	}
	Firmware struct {
		StorageDir string
	}
	GoogleAuth struct {
		ClientID     string
		ClientSecret string
		RedirectURI  string
	}
	Pprof struct {
		Enabled bool
		Addr    string
	}
}

func LoadConfig() *Config {
	// Try loading .env file if it exists, but don't fail if it doesn't
	_ = godotenv.Load()

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Defaults — every key must be registered for AutomaticEnv + Unmarshal to work
	viper.SetDefault("server.port", "8080")
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.profile", "default")
	viper.SetDefault("database.host", "localhost")
	viper.SetDefault("database.port", "5432")
	viper.SetDefault("database.user", "")
	viper.SetDefault("database.password", "")
	viper.SetDefault("database.name", "")
	viper.SetDefault("mqtt.broker", "tcp://localhost:1883")
	viper.SetDefault("mqtt.host", "")
	viper.SetDefault("mqtt.port", "")
	viper.SetDefault("mqtt.clientid", "iot-backend")
	viper.SetDefault("mqtt.username", "")
	viper.SetDefault("mqtt.password", "")
	viper.SetDefault("mqtt.publish_timeout", "5s")
	viper.SetDefault("mqtt.publish_retries", 3)
	viper.SetDefault("mqtt.publish_retry_delay", "500ms")
	viper.SetDefault("frontend.allowed_origins", "http://localhost,http://localhost:8080,http://127.0.0.1,http://127.0.0.1:8080,https://localhost,https://127.0.0.1")
	viper.SetDefault("session.cookie_name", "user_session")
	viper.SetDefault("session.domain", "")
	viper.SetDefault("session.secure", true)
	viper.SetDefault("session.same_site", "Lax")
	viper.SetDefault("valkey.addr", "localhost:6379")
	viper.SetDefault("valkey.password", "")
	viper.SetDefault("oauth.client_id", "google-client")
	viper.SetDefault("oauth.client_secret", "")
	viper.SetDefault("firmware.storage_dir", "./firmware")
	viper.SetDefault("googleauth.client_id", "")
	viper.SetDefault("googleauth.client_secret", "")
	viper.SetDefault("googleauth.redirect_uri", "")
	viper.SetDefault("pprof.enabled", false)
	viper.SetDefault("pprof.addr", "127.0.0.1:6060")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			log.Fatalf("Error reading config file: %v", err)
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		log.Fatalf("Unable to decode into struct, %v", err)
	}

	return &config
}

func (c *Config) FrontendAllowedOrigins() []string {
	raw := strings.TrimSpace(c.Frontend.AllowedOrigins)
	if raw == "" {
		return []string{"http://localhost"}
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin != "" {
			origins = append(origins, origin)
		}
	}

	if len(origins) == 0 {
		return []string{"http://localhost"}
	}

	return origins
}

func (c *Config) MQTTHostAndPort() (string, string) {
	host := strings.TrimSpace(c.MQTT.Host)
	port := strings.TrimSpace(c.MQTT.Port)

	if host == "" || port == "" {
		broker := strings.TrimSpace(c.MQTT.Broker)
		if idx := strings.Index(broker, "://"); idx >= 0 {
			broker = broker[idx+3:]
		}
		if slash := strings.Index(broker, "/"); slash >= 0 {
			broker = broker[:slash]
		}

		if parsedHost, parsedPort, err := net.SplitHostPort(broker); err == nil {
			if host == "" {
				host = parsedHost
			}
			if port == "" {
				port = parsedPort
			}
		} else if host == "" && broker != "" {
			host = broker
		}
	}

	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "1883"
	}

	return host, port
}

func (c *Config) SessionTTL() time.Duration {
	return 7 * 24 * time.Hour
}

func (c *Config) SessionSameSite() http.SameSite {
	switch strings.ToLower(strings.TrimSpace(c.Session.SameSite)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	case "default":
		return http.SameSiteDefaultMode
	default:
		return http.SameSiteLaxMode
	}
}

func (c *Config) FirmwareStoragePath() string {
	path := strings.TrimSpace(c.Firmware.StorageDir)
	if path == "" {
		return "./firmware"
	}
	return path
}
