package pkg

type Config struct {
	Package struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
		Epoch   string `yaml:"epoch"`
	}
	Environment struct {
		Contents struct {
			Packages []string
		}
	}
	Pipeline []struct {
		Uses string
		With map[string]string
	}
	Subpackages []struct {
		Name string
	}
}

type URI struct {
	URI, ExpectedSHA256, ExpectedSHA512 string
}

func (c Config) URIs() []URI {
	var uris []URI
	for _, s := range c.Pipeline {
		if s.Uses == "fetch" {
			uris = append(uris, URI{
				URI:            s.With["uri"],
				ExpectedSHA256: s.With["expected-sha256"],
				ExpectedSHA512: s.With["expected-sha512"],
			})
		}
	}
	return uris
}
