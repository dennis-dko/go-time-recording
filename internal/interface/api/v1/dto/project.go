package dto

// Project data transfer object
type Project struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	StartDate   string  `json:"startDate"`
	EndDate     *string `json:"endDate,omitempty"`
	Status      string  `json:"status"`
}

func (p *Project) RestPath() string {
	return "api/v1/projects"
}
