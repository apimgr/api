package config

// AnnouncementsConfig controls public site-wide messages.
type AnnouncementsConfig struct {
	Enabled  bool                  `yaml:"enabled"`
	Messages []AnnouncementMessage `yaml:"messages"`
}

// AnnouncementMessage is one scheduled public site banner.
type AnnouncementMessage struct {
	ID          string `yaml:"id"`
	Type        string `yaml:"type"`
	Title       string `yaml:"title"`
	Message     string `yaml:"message"`
	Start       string `yaml:"start"`
	End         string `yaml:"end"`
	Dismissible bool   `yaml:"dismissible"`
}
