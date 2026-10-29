package fileschema

import (
	"github.com/mclucy/lucy/internal/fn"
)

type FileMcdrPluginIdentifier struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	Description struct {
		EnUs string `json:"en_us"`
		ZhCn string `json:"zh_cn"`
	} `json:"description"`
	Author       fn.SingleOrSlice[string] `json:"author"`
	Link         string                   `json:"link"`
	Dependencies map[string]string        `json:"dependencies"`
	Resources    []string                 `json:"-"`
}
