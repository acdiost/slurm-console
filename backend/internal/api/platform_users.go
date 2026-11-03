package api

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"net/mail"
	"strconv"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/platformsettings"
	"slurm-console/backend/internal/rbac"
)

type platformUser struct {
	ID         string `json:"id"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	Username   string `json:"username"`
	AuthSource string `json:"authSource"`
	Active     bool   `json:"active"`
	Revision   int    `json:"revision"`
	UpdatedAt  string `json:"updatedAt"`
}

func platformUserView(r *core.Record) platformUser {
	return platformUser{r.Id, r.Email(), r.GetString("name"), r.GetString("username"), r.GetString("auth_source"), r.GetBool("active"), r.GetInt("management_revision"), r.GetString("updated")}
}
func registerPlatformUserRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	u := r.Group("/platform-users")
	u.BindFunc(rbac.Require("platform.users.manage", false))
	u.GET("", listPlatformUsers)
	u.POST("", createPlatformUser)
	u.PATCH("/{userId}", updatePlatformUser)
	u.GET("/{userId}/roles", platformUserRoles).BindFunc(rbac.Require("platform.roles.manage", false))
	u.POST("/{userId}/roles", func(e *core.RequestEvent) error { return changePlatformRole(e, false) }).BindFunc(rbac.Require("platform.roles.manage", false))
	u.POST("/{userId}/roles/remove", func(e *core.RequestEvent) error { return changePlatformRole(e, true) }).BindFunc(rbac.Require("platform.roles.manage", false))
	r.GET("/platform-role-catalog", platformRoleCatalog).BindFunc(rbac.Require("platform.roles.manage", false))
}
func listPlatformUsers(e *core.RequestEvent) error {
	if err := slurmQuery(e, "page", "perPage", "q", "source", "active"); err != nil {
		return err
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	q := e.Request.URL.Query()
	text := q.Get("q")
	source := q.Get("source")
	active := q.Get("active")
	if !platformText(text, 0, 100, false) || source != "" && source != "local" && source != "pam" && source != "ldap" || active != "" && active != "true" && active != "false" {
		return e.BadRequestError("用户筛选条件不合法。", nil)
	}
	filters := []dbx.Expression{dbx.NewExp("1=1")}
	if text != "" {
		filters = append(filters, dbx.NewExp("instr(lower(email),lower({:q}))>0 OR instr(lower(name),lower({:q}))>0 OR instr(lower(username),lower({:q}))>0", dbx.Params{"q": text}))
	}
	if source != "" {
		filters = append(filters, dbx.HashExp{"auth_source": source})
	}
	if active != "" {
		filters = append(filters, dbx.HashExp{"active": active == "true"})
	}
	expr := dbx.And(filters...)
	total, err := e.App.CountRecords("users", expr)
	if err != nil {
		return e.Error(503, "用户读取失败。", nil)
	}
	rows := []*core.Record{}
	err = e.App.RecordQuery("users").Where(expr).OrderBy("email", "id").Limit(int64(size)).Offset(int64((page - 1) * size)).All(&rows)
	if err != nil {
		return e.Error(503, "用户读取失败。", nil)
	}
	items := []platformUser{}
	for _, r := range rows {
		items = append(items, platformUserView(r))
	}
	if err = platformFresh(e, e.App, "platform.users.manage"); err != nil {
		return err
	}
	return response(e, map[string]any{"items": items, "page": page, "perPage": size, "totalItems": total})
}
func createPlatformUser(e *core.RequestEvent) error {
	var in struct {
		Email      string `json:"email"`
		Name       string `json:"name"`
		AuthSource string `json:"authSource"`
		Username   string `json:"username"`
		Password   string `json:"password"`
	}
	if err := platformBody(e, &in, "email", "name", "authSource", "username", "password"); err != nil {
		return err
	}
	address, err := mail.ParseAddress(in.Email)
	if err != nil || address.Address != in.Email || len(in.Email) > 255 || !platformText(in.Email, 3, 255, false) || !platformText(in.Name, 1, 100, false) {
		return e.BadRequestError("邮箱或显示名称不合法。", nil)
	}
	entryID := ""
	if in.AuthSource == "local" {
		if in.Username != "" || !utf8.ValidString(in.Password) || utf8.RuneCountInString(in.Password) < 12 || len(in.Password) > 71 {
			return e.BadRequestError("平台账户使用邮箱登录，密码至少 12 个字符且不超过 71 字节。", nil)
		}
	} else if in.AuthSource == "pam" || in.AuthSource == "ldap" {
		if in.Password != "" {
			return e.BadRequestError("系统或目录账户不接收外部密码。", nil)
		}
		entryID, err = auth.LookupExternalIdentity(e.Request.Context(), e.App, in.AuthSource, in.Username)
		if err != nil {
			return e.BadRequestError("外部身份不可用，请核对配置、用户名与账户准入条件。", nil)
		}
	} else {
		return e.BadRequestError("认证来源不合法。", nil)
	}
	var saved platformUser
	err = audit.Mutate(e.App, audit.PlatformActor(e.Auth, e.Get("requestId").(string)), func(tx core.App) (audit.Event, error) {
		if err := platformFresh(e, tx, "platform.users.manage"); err != nil {
			return audit.Event{}, err
		}
		c, err := tx.FindCollectionByNameOrId("users")
		if err != nil {
			return audit.Event{}, err
		}
		r := core.NewRecord(c)
		r.SetEmail(in.Email)
		r.Set("name", in.Name)
		r.Set("auth_source", in.AuthSource)
		r.Set("username", in.Username)
		r.Set("ldap_entry_id", entryID)
		r.Set("active", true)
		r.Set("management_revision", 1)
		if in.AuthSource == "local" {
			r.SetPassword(in.Password)
		} else {
			r.SetPassword(rand.Text() + rand.Text())
		}
		if err = tx.Save(r); err != nil {
			return audit.Event{}, e.BadRequestError("用户创建失败，请检查邮箱、用户名是否重复及输入格式。", nil)
		}
		saved = platformUserView(r)
		return audit.Event{Action: "users.create", TargetType: "user", TargetID: r.Id, Changes: []audit.Change{{Field: "authSource", After: in.AuthSource}, {Field: "username", After: in.Username}, {Field: "userName", After: in.Name}}, Message: "已创建平台账户；未自动授予角色或 Slurm 身份。"}, nil
	})
	in.Password = ""
	if err != nil {
		return platformFailure(e, err)
	}
	return response(e, saved)
}
func managedUser(e *core.RequestEvent, app core.App) (*core.Record, error) {
	id := e.Request.PathValue("userId")
	if !configVersionID.MatchString(id) {
		return nil, e.BadRequestError("用户标识不合法。", nil)
	}
	r, err := app.FindRecordById("users", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, e.NotFoundError("用户不存在。", nil)
	}
	return r, err
}
func lastAdmin(app core.App, id string) (bool, error) {
	rows, err := app.FindRecordsByFilter("user_platform_roles", "role.code='super_admin' && user.active=true", "", 0, 0)
	if err != nil {
		return false, err
	}
	target := false
	other := false
	for _, r := range rows {
		if r.GetString("user") == id {
			target = true
		} else {
			other = true
		}
	}
	return target && !other, nil
}
func updatePlatformUser(e *core.RequestEvent) error {
	var in struct {
		Name         string `json:"name"`
		Active       bool   `json:"active"`
		Revision     int    `json:"revision"`
		Confirmation string `json:"confirmation"`
	}
	if err := platformBody(e, &in, "name", "active", "revision", "confirmation"); err != nil {
		return err
	}
	if !platformText(in.Name, 1, 100, false) || in.Revision < 0 || in.Confirmation != "UPDATE USER" {
		return e.BadRequestError("请核对用户并输入 UPDATE USER。", nil)
	}
	var saved platformUser
	err := audit.Mutate(e.App, audit.PlatformActor(e.Auth, e.Get("requestId").(string)), func(tx core.App) (audit.Event, error) {
		if err := platformFresh(e, tx, "platform.users.manage"); err != nil {
			return audit.Event{}, err
		}
		r, err := managedUser(e, tx)
		if err != nil {
			return audit.Event{}, err
		}
		if r.GetInt("management_revision") != in.Revision {
			return audit.Event{}, platformsettings.ErrConflict
		}
		if !in.Active {
			if r.Id == e.Auth.Id {
				return audit.Event{}, e.BadRequestError("不能停用当前登录账户。", nil)
			}
			last, err := lastAdmin(tx, r.Id)
			if err != nil {
				return audit.Event{}, err
			}
			if last {
				return audit.Event{}, e.BadRequestError("不能停用最后一个活动 Super Admin。", nil)
			}
		}
		changes := []audit.Change{}
		if r.GetString("name") != in.Name {
			changes = append(changes, audit.Change{Field: "userName", Before: r.GetString("name"), After: in.Name})
		}
		if r.GetBool("active") != in.Active {
			changes = append(changes, audit.Change{Field: "active", Before: strconv.FormatBool(r.GetBool("active")), After: strconv.FormatBool(in.Active)})
			r.RefreshTokenKey()
		}
		if len(changes) == 0 {
			return audit.Event{}, e.BadRequestError("用户没有变化。", nil)
		}
		r.Set("name", in.Name)
		r.Set("active", in.Active)
		r.Set("management_revision", in.Revision+1)
		if err = tx.Save(r); err != nil {
			return audit.Event{}, err
		}
		saved = platformUserView(r)
		return audit.Event{Action: "users.update", TargetType: "user", TargetID: r.Id, Changes: changes, Message: "已更新平台用户；停用/启用时撤销既有会话令牌。"}, nil
	})
	if err != nil {
		return platformFailure(e, err)
	}
	return response(e, saved)
}

type userRole struct {
	RoleCode    string `json:"roleCode"`
	RoleName    string `json:"roleName"`
	Scope       string `json:"scope"`
	ClusterID   string `json:"clusterId"`
	ClusterName string `json:"clusterName"`
}

func rolesForUser(app core.App, id string) ([]userRole, error) {
	result := []userRole{}
	for _, collection := range []string{"user_platform_roles", "user_cluster_roles"} {
		rows, err := app.FindRecordsByFilter(collection, "user={:id}", "id", 1001, 0, dbx.Params{"id": id})
		if err != nil {
			return nil, err
		}
		if len(rows) > 1000 {
			return nil, errors.New("role binding limit")
		}
		for _, r := range rows {
			role, err := app.FindRecordById("roles", r.GetString("role"))
			if err != nil {
				return nil, err
			}
			v := userRole{RoleCode: role.GetString("code"), RoleName: role.GetString("name"), Scope: role.GetString("scope"), ClusterID: r.GetString("cluster")}
			if v.ClusterID != "" {
				c, err := app.FindRecordById("clusters", v.ClusterID)
				if err != nil {
					return nil, err
				}
				v.ClusterName = c.GetString("name")
			}
			result = append(result, v)
		}
	}
	return result, nil
}
func platformUserRoles(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	r, err := managedUser(e, e.App)
	if err != nil {
		return err
	}
	roles, err := rolesForUser(e.App, r.Id)
	if err != nil {
		return e.Error(503, "用户授权读取失败。", nil)
	}
	if err = platformFresh(e, e.App, "platform.users.manage", "platform.roles.manage"); err != nil {
		return err
	}
	return response(e, map[string]any{"user": platformUserView(r), "roles": roles})
}
func platformRoleCatalog(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	roles, err := e.App.FindRecordsByFilter("roles", "", "scope,code", 201, 0)
	if err != nil || len(roles) > 200 {
		return e.Error(503, "角色目录不可用或超过上限。", nil)
	}
	items := []map[string]any{}
	for _, r := range roles {
		rows, err := e.App.FindRecordsByFilter("role_permissions", "role={:role}", "id", 1001, 0, dbx.Params{"role": r.Id})
		if err != nil || len(rows) > 1000 {
			return e.Error(503, "权限目录不可用或超过上限。", nil)
		}
		permissions := []map[string]string{}
		for _, b := range rows {
			p, err := e.App.FindRecordById("permissions", b.GetString("permission"))
			if err != nil {
				return e.Error(503, "权限目录不可用。", nil)
			}
			permissions = append(permissions, map[string]string{"code": p.GetString("code"), "name": p.GetString("name")})
		}
		items = append(items, map[string]any{"code": r.GetString("code"), "name": r.GetString("name"), "scope": r.GetString("scope"), "permissions": permissions})
	}
	clusters, err := e.App.FindRecordsByFilter("clusters", "", "name,id", 1001, 0)
	if err != nil || len(clusters) > 1000 {
		return e.Error(503, "授权集群目录不可用或超过上限。", nil)
	}
	cs := []map[string]string{}
	for _, c := range clusters {
		cs = append(cs, map[string]string{"id": c.Id, "name": c.GetString("name")})
	}
	if err = platformFresh(e, e.App, "platform.roles.manage"); err != nil {
		return err
	}
	return response(e, map[string]any{"roles": items, "clusters": cs})
}
func changePlatformRole(e *core.RequestEvent, remove bool) error {
	var in struct {
		RoleCode     string `json:"roleCode"`
		ClusterID    string `json:"clusterId"`
		Revision     int    `json:"revision"`
		Confirmation string `json:"confirmation"`
	}
	if err := platformBody(e, &in, "roleCode", "clusterId", "revision", "confirmation"); err != nil {
		return err
	}
	if in.Confirmation != "CHANGE ACCESS" || !platformText(in.RoleCode, 1, 64, false) || in.ClusterID != "" && !configVersionID.MatchString(in.ClusterID) || in.Revision < 0 {
		return e.BadRequestError("须核对角色及集群范围并输入 CHANGE ACCESS。", nil)
	}
	var saved platformUser
	err := audit.Mutate(e.App, audit.PlatformActor(e.Auth, e.Get("requestId").(string)), func(tx core.App) (audit.Event, error) {
		if err := platformFresh(e, tx, "platform.users.manage", "platform.roles.manage"); err != nil {
			return audit.Event{}, err
		}
		u, err := managedUser(e, tx)
		if err != nil {
			return audit.Event{}, err
		}
		if u.GetInt("management_revision") != in.Revision {
			return audit.Event{}, platformsettings.ErrConflict
		}
		role, err := tx.FindFirstRecordByData("roles", "code", in.RoleCode)
		if err != nil {
			return audit.Event{}, e.BadRequestError("角色不存在。", nil)
		}
		scope := role.GetString("scope")
		if scope == "platform" && in.ClusterID != "" || scope == "cluster" && in.ClusterID == "" {
			return audit.Event{}, e.BadRequestError("平台角色不能指定集群，集群角色必须指定集群。", nil)
		}
		name, filter := "user_platform_roles", "user={:user} && role={:role}"
		params := dbx.Params{"user": u.Id, "role": role.Id}
		if scope == "cluster" {
			if _, err := tx.FindRecordById("clusters", in.ClusterID); err != nil {
				return audit.Event{}, e.BadRequestError("集群不存在。", nil)
			}
			name = "user_cluster_roles"
			filter += " && cluster={:cluster}"
			params["cluster"] = in.ClusterID
		}
		row, err := tx.FindFirstRecordByFilter(name, filter, params)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return audit.Event{}, err
		}
		action, before, after := "access.grant", "", in.RoleCode
		if remove {
			if row == nil {
				return audit.Event{}, e.NotFoundError("该授权已不存在。", nil)
			}
			if in.RoleCode == "super_admin" {
				last, err := lastAdmin(tx, u.Id)
				if err != nil {
					return audit.Event{}, err
				}
				if last {
					return audit.Event{}, e.BadRequestError("不能撤销最后一个活动 Super Admin。", nil)
				}
			}
			if err = tx.Delete(row); err != nil {
				return audit.Event{}, err
			}
			action = "access.revoke"
			before = in.RoleCode
			after = ""
		} else {
			if row != nil {
				return audit.Event{}, e.Error(409, "该角色已授予，请刷新核对。", nil)
			}
			n, err := tx.CountRecords(name, dbx.HashExp{"user": u.Id})
			if err != nil {
				return audit.Event{}, err
			}
			if n >= 1000 {
				return audit.Event{}, e.BadRequestError("用户角色绑定已达上限。", nil)
			}
			if _, err = rbac.Grant(tx, u.Id, in.RoleCode, in.ClusterID); err != nil {
				return audit.Event{}, err
			}
		}
		u.Set("management_revision", in.Revision+1)
		if err = tx.Save(u); err != nil {
			return audit.Event{}, err
		}
		saved = platformUserView(u)
		return audit.Event{Action: action, ClusterID: in.ClusterID, TargetType: "user", TargetID: u.Id, Changes: []audit.Change{{Field: "role", Before: before, After: after}}, Message: "平台角色授权已更新；未修改 Slurm 原生身份或权限。"}, nil
	})
	if err != nil {
		return platformFailure(e, err)
	}
	return response(e, saved)
}
