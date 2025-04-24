package release

import (
	"testing"
	"time"
)

func TestGenerate(t *testing.T) {

	tm := time.Unix(1744299341, 0)

	res, err := Generate(Release{
		Origin:        "mackerel",
		Label:         "mackerel",
		Suite:         "stable",
		CodeName:      "mackerel",
		Date:          tm.Format(time.RFC1123),
		Architectures: []string{"amd64", "arm64"},
		Components:    "contrib",
		Description:   "mackerel repository for Debian",
		MD5Sum: []Hash{
			{
				Hash: "4ca5b53feff0bcb5856d6da3144e397d", Size: 2210, Filename: "contrib/binary-amd64/Packages",
			},
		},
		SHA1: []Hash{
			{
				Hash: "39d8396d0bfcacc299b5dfb39f5826b796edca18", Size: 2210, Filename: "contrib/binary-amd64/Packages",
			},
		},
		SHA256: []Hash{
			{
				Hash: "c853aa191454968fbedb1110432e6f9bce097c4a4fba7b7105b80c2d179437eb", Size: 2210, Filename: "contrib/binary-amd64/Packages",
			},
		},
	})

	if err != nil {
		t.Fatal(err)
	}

	// TODO improve tests.
	t.Log(res)

}
