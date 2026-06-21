module cclaw

go 1.26

require my.favorite/platformutility v0.0.0

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/go-ini/ini v1.67.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace my.favorite/platformutility => ./platformutility
