package clipboard

import (
	"reflect"
	"testing"
)

func TestParseCurl(t *testing.T) {
	cmd := `curl 'https://i.ytimg.com/an_webp/8gaSCj1-6ck/mqdefault_6s.webp?du=3000' \
  -H 'sec-ch-ua-platform: "Linux"' \
  -H 'Referer: https://www.youtube.com/'`
  
	url, headers := ParseCurl(cmd)
	if url != "https://i.ytimg.com/an_webp/8gaSCj1-6ck/mqdefault_6s.webp?du=3000" {
		t.Errorf("unexpected URL: %v", url)
	}

	expectedHeaders := map[string]string{
		"sec-ch-ua-platform": `"Linux"`,
		"Referer":            "https://www.youtube.com/",
	}

	if !reflect.DeepEqual(headers, expectedHeaders) {
		t.Errorf("unexpected headers: %v", headers)
	}
}
