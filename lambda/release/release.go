package release

import (
	"bytes"
	"strings"
	"text/template"
)

const releaseTemplate = `Origin: {{.Origin}}
Label: {{.Label}}
Suite: {{.Suite}}
Codename: {{.CodeName}}
Date: {{.Date}}
Architectures: {{join .Architectures " "}}
Components: {{.Components}}
Description: {{.Description}}
MD5Sum:
{{range .MD5Sum}} {{.Hash}} {{.Size}} {{.Filename}}
{{end -}}
SHA1:
{{range .SHA1}} {{.Hash}} {{.Size}} {{.Filename}}
{{end -}}
SHA256:
{{range .SHA256}} {{.Hash}} {{.Size}} {{.Filename}}
{{end -}}
`

type Release struct {
	Origin        string
	Label         string
	Suite         string
	CodeName      string
	Date          string
	Architectures []string
	Components    string
	Description   string
	MD5Sum        []Hash
	SHA1          []Hash
	SHA256        []Hash
}

type Hash struct {
	Hash     string
	Size     int64
	Filename string
}

func Generate(r Release) (string, error) {
	funcs := template.FuncMap{"join": strings.Join}
	t := template.Must(template.New("release").Funcs(funcs).Parse(releaseTemplate))

	buf := bytes.NewBuffer([]byte{})
	if err := t.Execute(buf, r); err != nil {
		return "", err
	}
	return buf.String(), nil
}
