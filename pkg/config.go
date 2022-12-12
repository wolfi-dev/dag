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
