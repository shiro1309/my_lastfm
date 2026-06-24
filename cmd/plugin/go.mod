module fm-scraper/plugin

go 1.25.0

require (
	fm-scraper v0.0.0
	github.com/extism/go-pdk v1.1.3
	github.com/navidrome/navidrome/plugins/pdk/go v0.0.0-00010101000000-000000000000
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	fm-scraper => ../..
	github.com/navidrome/navidrome/plugins/pdk/go => /root/navidrome/plugins/pdk/go
)
