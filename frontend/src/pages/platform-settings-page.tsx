import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { useAuth } from "@/stores/auth"
import { BasicSettings } from "@/features/platform-settings/basic"
import { IntegrationSettings } from "@/features/platform-settings/ai"
import { UserSettings } from "@/features/platform-settings/users"
import { RuntimeSettings } from "@/features/platform-settings/runtime"
export function PlatformSettingsPage() {
  const user = useAuth((s) => s.user),
    permissions = user?.access.platformPermissions || []
  const read = permissions.includes("platform.settings.read"),
    manage = permissions.includes("platform.settings.manage"),
    users = permissions.includes("platform.users.manage"),
    roles = permissions.includes("platform.roles.manage")
  if (!user || (!read && !users && !roles))
    return (
      <div role="alert" className="p-8 text-sm">
        当前没有平台设置或用户授权管理权限。
      </div>
    )
  return (
    <div
      key={`${user.id}:${permissions.join(",")}`}
      className="mx-auto max-w-6xl space-y-6 p-6 sm:p-8"
    >
      <header>
        <p className="font-mono text-micro tracking-widest text-muted-foreground">
          PLATFORM ADMINISTRATION
        </p>
        <h1 className="mt-2 text-2xl font-semibold">平台设置</h1>
        <p className="mt-2 text-sm leading-7 text-muted-foreground">
          维护平台信息、集成配置、用户授权，并查看当前运行状态。
        </p>
      </header>
      <Tabs defaultValue={read ? "basic" : "users"}>
        <TabsList className="h-auto flex-wrap" aria-label="平台设置分类">
          {read && (
            <>
              <TabsTrigger value="basic">基础信息</TabsTrigger>
              <TabsTrigger value="integrations">集成配置</TabsTrigger>
            </>
          )}
          {(users || roles) && (
            <TabsTrigger value="users">用户与角色</TabsTrigger>
          )}
          {read && <TabsTrigger value="runtime">运行信息</TabsTrigger>}
        </TabsList>
        {read && (
          <>
            <TabsContent value="basic" className="mt-4">
              <BasicSettings actor={user.id} manage={manage} />
            </TabsContent>
            <TabsContent value="integrations" className="mt-4">
              <IntegrationSettings actor={user.id} manage={manage} />
            </TabsContent>
            <TabsContent value="runtime" className="mt-4">
              <RuntimeSettings actor={user.id} />
            </TabsContent>
          </>
        )}
        {(users || roles) && (
          <TabsContent value="users" className="mt-4">
            <UserSettings actor={user.id} canUsers={users} canRoles={roles} />
          </TabsContent>
        )}
      </Tabs>
    </div>
  )
}
