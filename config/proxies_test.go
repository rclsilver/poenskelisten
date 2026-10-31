package config

import (
	"reflect"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{"empty trusts nothing", "", []string{}, false},
		{"blank entries are ignored", " , ,", []string{}, false},
		{"single address", "10.0.0.5", []string{"10.0.0.5"}, false},
		{"range is normalized", "10.244.1.7/16", []string{"10.244.0.0/16"}, false},
		{"mixed list with spaces", "127.0.0.1, 172.16.0.0/12 ,::1", []string{"127.0.0.1", "172.16.0.0/12", "::1"}, false},
		{"ipv6 range", "fd00::/8", []string{"fd00::/8"}, false},
		{"hostname is refused", "traefik", nil, true},
		{"bad range is refused", "10.0.0.0/33", nil, true},
		{"one bad entry fails the list", "10.0.0.1,nope", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseTrustedProxies(c.raw)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if !c.wantErr && !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
