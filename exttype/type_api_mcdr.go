package exttype

type ApiMcdrPlugin struct {
	Id      string `json:"id"`
	Authors []struct {
		Name string `json:"name"`
		Link string `json:"link"`
	} `json:"authors"`
	Repository   string   `json:"repository"`
	Branch       string   `json:"branch"`
	RelatedPath  string   `json:"related_path"`
	Labels       []string `json:"labels"`
	Introduction struct {
		EnUs string `json:"en_us"`
		ZhCn string `json:"zh_cn"`
	} `json:"introduction"`
}
