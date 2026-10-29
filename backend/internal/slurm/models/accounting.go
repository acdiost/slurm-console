package models

type Account struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	Organization     string `json:"organization"`
	AssociationCount int    `json:"associationCount"`
	UserCount        int    `json:"userCount"`
}
type SlurmUser struct {
	Name                string   `json:"name"`
	DefaultAccount      string   `json:"defaultAccount"`
	DefaultAccountScope string   `json:"defaultAccountScope"`
	AdministratorLevel  []string `json:"administratorLevel"`
	AssociationCount    int      `json:"associationCount"`
}
type Association struct {
	ID            string `json:"id"`
	Cluster       string `json:"cluster"`
	Account       string `json:"account"`
	User          string `json:"user"`
	Partition     string `json:"partition"`
	ParentAccount string `json:"parentAccount"`
	IsDefault     bool   `json:"isDefault"`
	Comment       string `json:"comment"`
}
