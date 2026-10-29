package adapter

import (
	"context"
	"slices"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"strconv"
)

type AccountingSnapshot struct {
	Cluster      string
	Accounts     []models.Account
	Users        []models.SlurmUser
	Associations []models.Association
	QOS          []models.QOS
}

func (a *Adapter) AccountingCluster(ctx context.Context) (string, error) {
	h, err := a.Health(ctx)
	if err != nil {
		return "", err
	}
	if !client.ValidAccountingName(h.Cluster) {
		return "", client.Failure("accounting_unavailable", "控制器未返回有效的 Slurm 集群标识，无法隔离记账数据。")
	}
	return h.Cluster, nil
}
func (a *Adapter) AllAccounts(ctx context.Context) ([]models.Account, error) {
	envelope, err := a.reader.Read(ctx, client.DBAccounts, "")
	if err != nil {
		return nil, err
	}
	values, err := decode[struct {
		Name, Description, Organization string
		Flags                           []string
	}](envelope.Accounts)
	if err != nil {
		return nil, err
	}
	result := []models.Account{}
	seen := map[string]bool{}
	for _, v := range values {
		if slices.Contains(v.Flags, "DELETED") {
			continue
		}
		if !client.ValidAccountingName(v.Name) || seen[v.Name] || len(v.Description) > 4096 || len(v.Organization) > 4096 {
			return nil, invalid()
		}
		seen[v.Name] = true
		result = append(result, models.Account{Name: v.Name, Description: v.Description, Organization: v.Organization})
	}
	return result, nil
}
func (a *Adapter) AllUsers(ctx context.Context) ([]models.SlurmUser, error) {
	envelope, err := a.reader.Read(ctx, client.DBUsers, "")
	if err != nil {
		return nil, err
	}
	values, err := decode[struct {
		Name               string
		Flags              []string
		AdministratorLevel []string `json:"administrator_level"`
		Default            struct{ Account string }
	}](envelope.Users)
	if err != nil {
		return nil, err
	}
	result := []models.SlurmUser{}
	seen := map[string]bool{}
	for _, v := range values {
		if slices.Contains(v.Flags, "DELETED") {
			continue
		}
		if !client.ValidAccountingName(v.Name) || seen[v.Name] || len(v.Default.Account) > 64 || len(v.AdministratorLevel) > 4 {
			return nil, invalid()
		}
		for _, level := range v.AdministratorLevel {
			if !slices.Contains([]string{"Not Set", "None", "Operator", "Administrator"}, level) {
				return nil, invalid()
			}
		}
		seen[v.Name] = true
		if v.AdministratorLevel == nil {
			v.AdministratorLevel = []string{}
		}
		result = append(result, models.SlurmUser{Name: v.Name, DefaultAccount: v.Default.Account, AdministratorLevel: v.AdministratorLevel})
	}
	return result, nil
}
func (a *Adapter) AssociationsIn(ctx context.Context, cluster string) ([]models.Association, error) {
	envelope, err := a.reader.Read(ctx, client.DBAssociations, cluster)
	if err != nil {
		return nil, err
	}
	values, err := decode[struct {
		ID                                         *int64
		Cluster, Account, User, Partition, Comment string
		Parent                                     string `json:"parent_account"`
		IsDefault                                  bool   `json:"is_default"`
		Flags                                      []string
		QOS                                        []string
		Default                                    struct{ QOS string }
	}](envelope.Associations)
	if err != nil {
		return nil, err
	}
	result := []models.Association{}
	seen := map[int64]bool{}
	for _, v := range values {
		// Slurm may ignore a query selector; enforce cluster isolation locally too.
		if v.Cluster != cluster || slices.Contains(v.Flags, "DELETED") {
			continue
		}
		if v.ID == nil || *v.ID <= 0 || *v.ID > 4294967295 || seen[*v.ID] || !client.ValidAccountingName(v.Account) || v.User != "" && !client.ValidAccountingName(v.User) || v.Partition != "" && !client.ValidAccountingName(v.Partition) || len(v.Comment) > 4096 || len(v.Parent) > 64 {
			return nil, invalid()
		}
		seen[*v.ID] = true
		if len(v.QOS) > 20000 || v.Default.QOS != "" && !client.ValidAccountingName(v.Default.QOS) {
			return nil, invalid()
		}
		for _, q := range v.QOS {
			if !client.ValidAccountingName(q) {
				return nil, invalid()
			}
		}
		if v.QOS == nil {
			v.QOS = []string{}
		}
		result = append(result, models.Association{ID: strconv.FormatInt(*v.ID, 10), Cluster: v.Cluster, Account: v.Account, User: v.User, Partition: v.Partition, ParentAccount: v.Parent, IsDefault: v.IsDefault, Comment: v.Comment, QOS: v.QOS, DefaultQOS: v.Default.QOS})
	}
	return result, nil
}
func (a *Adapter) GetAssociations(ctx context.Context) ([]models.Association, error) {
	cluster, err := a.AccountingCluster(ctx)
	if err != nil {
		return nil, err
	}
	return a.AssociationsIn(ctx, cluster)
}
func FilterAccounts(accounts []models.Account, assocs []models.Association) []models.Account {
	counts := map[string]int{}
	users := map[string]map[string]bool{}
	for _, v := range assocs {
		counts[v.Account]++
		if users[v.Account] == nil {
			users[v.Account] = map[string]bool{}
		}
		if v.User != "" {
			users[v.Account][v.User] = true
		}
	}
	result := []models.Account{}
	for _, v := range accounts {
		if counts[v.Name] > 0 {
			v.AssociationCount = counts[v.Name]
			v.UserCount = len(users[v.Name])
			result = append(result, v)
		}
	}
	return result
}
func FilterUsers(users []models.SlurmUser, assocs []models.Association) []models.SlurmUser {
	accounts := map[string]bool{}
	counts := map[string]int{}
	for _, v := range assocs {
		accounts[v.Account] = true
		if v.User != "" {
			counts[v.User]++
		}
	}
	result := []models.SlurmUser{}
	for _, v := range users {
		if counts[v.Name] > 0 {
			v.AssociationCount = counts[v.Name]
			v.DefaultAccountScope = "unset"
			if v.DefaultAccount != "" {
				if accounts[v.DefaultAccount] {
					v.DefaultAccountScope = "cluster"
				} else {
					v.DefaultAccountScope = "outside_cluster"
					v.DefaultAccount = ""
				}
			}
			result = append(result, v)
		}
	}
	return result
}
func (a *Adapter) GetAccounts(ctx context.Context) ([]models.Account, error) {
	assocs, err := a.GetAssociations(ctx)
	if err != nil {
		return nil, err
	}
	accounts, err := a.AllAccounts(ctx)
	if err != nil {
		return nil, err
	}
	return FilterAccounts(accounts, assocs), nil
}
func (a *Adapter) GetSlurmUsers(ctx context.Context) ([]models.SlurmUser, error) {
	assocs, err := a.GetAssociations(ctx)
	if err != nil {
		return nil, err
	}
	users, err := a.AllUsers(ctx)
	if err != nil {
		return nil, err
	}
	return FilterUsers(users, assocs), nil
}
func (a *Adapter) Accounting(ctx context.Context) (AccountingSnapshot, error) {
	result := AccountingSnapshot{}
	var err error
	result.Cluster, err = a.AccountingCluster(ctx)
	if err != nil {
		return result, err
	}
	result.Associations, err = a.AssociationsIn(ctx, result.Cluster)
	if err != nil {
		return result, err
	}
	result.Accounts, err = a.AllAccounts(ctx)
	if err != nil {
		return result, err
	}
	result.Users, err = a.AllUsers(ctx)
	return result, err
}
