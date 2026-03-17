package routing

type Condition struct {
	Field    string `json:"field"`    // dot-notation path
	Operator string `json:"operator"` // eq | neq | gt | gte | lt | lte | contains | exists | not_exists
	Value    any    `json:"value"`
}
