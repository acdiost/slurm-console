import { useForm, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { LoaderCircle, ShieldCheck } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  connectionFormSchema,
  type ClusterConfiguration,
  type ConnectionProfile,
  type ConnectionValues,
} from "./schema"

export function ConnectionForm({
  config,
  profiles,
  saving,
  error,
  onSave,
  onCancel,
}: {
  config: ClusterConfiguration | null
  profiles: ConnectionProfile[]
  saving: boolean
  error: string
  onSave: (values: ConnectionValues) => void
  onCancel: () => void
}) {
  const {
    register,
    control,
    setValue,
    handleSubmit,
    formState: { errors },
  } = useForm<ConnectionValues>({
    resolver: zodResolver(connectionFormSchema),
    defaultValues: {
      name: config?.name ?? "",
      slug: config?.slug ?? "",
      description: config?.description ?? "",
      connectionRef: config?.connectionRef || profiles[0]?.ref || "",
      apiVersion: config?.apiVersion || profiles[0]?.apiVersions[0] || "",
      enabled: config?.connectionRef ? config.enabled : true,
      timeoutSeconds: config?.timeoutSeconds ?? 5,
      revision: config?.revision ?? 0,
    },
  })
  const values = useWatch({ control })
  const profile = profiles.find((item) => item.ref === values.connectionRef)
  const fieldError = (name: keyof ConnectionValues) =>
    errors[name] && (
      <p
        id={`cluster-${name}-error`}
        className="text-2xs text-destructive"
        role="alert"
      >
        {errors[name]?.message}
      </p>
    )
  return (
    <form
      onSubmit={handleSubmit(onSave)}
      noValidate
      className="space-y-6 px-5 pb-6"
    >
      <fieldset disabled={saving} className="space-y-5 disabled:opacity-70">
        <div className="space-y-2">
          <Label htmlFor="cluster-name">集群名称</Label>
          <Input
            id="cluster-name"
            placeholder="例如：科研计算集群"
            aria-invalid={!!errors.name}
            aria-describedby="cluster-name-error"
            {...register("name")}
          />
          {fieldError("name")}
        </div>
        <div className="space-y-2">
          <Label htmlFor="cluster-slug">集群标识</Label>
          <Input
            id="cluster-slug"
            readOnly={!!config}
            placeholder="例如：research-a"
            className="font-mono"
            aria-invalid={!!errors.slug}
            aria-describedby="cluster-slug-error"
            {...register("slug")}
          />
          <p className="text-2xs text-muted-foreground">
            用于识别和授权，创建后保持不变。
          </p>
          {fieldError("slug")}
        </div>
        <div className="space-y-2">
          <Label htmlFor="cluster-description">说明</Label>
          <Input
            id="cluster-description"
            placeholder="集群用途或所在环境（可选）"
            aria-invalid={!!errors.description}
            {...register("description")}
          />
          {fieldError("description")}
        </div>
        <div className="space-y-2">
          <Label htmlFor="cluster-connectionRef">连接方式</Label>
          <Select
            value={values.connectionRef}
            onValueChange={(ref) => {
              setValue("connectionRef", ref, { shouldValidate: true })
              setValue(
                "apiVersion",
                profiles.find((item) => item.ref === ref)?.apiVersions[0] ?? "",
                { shouldValidate: true }
              )
            }}
          >
            <SelectTrigger
              id="cluster-connectionRef"
              className="w-full"
              aria-invalid={!!errors.connectionRef}
            >
              <SelectValue placeholder="选择连接方式" />
            </SelectTrigger>
            <SelectContent>
              {profiles.map((item) => (
                <SelectItem key={item.ref} value={item.ref}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {fieldError("connectionRef")}
          {profile && (
            <div className="rounded-md border bg-muted/30 p-3">
              <p className="font-mono text-2xs break-all text-muted-foreground">
                {profile.endpoint}
              </p>
              <p className="mt-2 text-2xs">
                {profile.authType === "local"
                  ? "本地身份认证 · Unix socket"
                  : "JWT 认证 · HTTPS"}
              </p>
            </div>
          )}
        </div>
        <div className="grid grid-cols-2 gap-4">
          <div className="space-y-2">
            <Label htmlFor="cluster-apiVersion">REST API 版本</Label>
            <Select
              value={values.apiVersion}
              onValueChange={(value) =>
                setValue("apiVersion", value, { shouldValidate: true })
              }
            >
              <SelectTrigger
                id="cluster-apiVersion"
                className="w-full"
                aria-invalid={!!errors.apiVersion}
              >
                <SelectValue placeholder="选择 API 版本" />
              </SelectTrigger>
              <SelectContent>
                {profile?.apiVersions.map((version) => (
                  <SelectItem key={version} value={version}>
                    {version}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {fieldError("apiVersion")}
          </div>
          <div className="space-y-2">
            <Label htmlFor="cluster-timeoutSeconds">连接超时（秒）</Label>
            <Input
              id="cluster-timeoutSeconds"
              type="number"
              min={1}
              max={30}
              aria-invalid={!!errors.timeoutSeconds}
              aria-describedby="cluster-timeoutSeconds-error"
              {...register("timeoutSeconds", { valueAsNumber: true })}
            />
            {fieldError("timeoutSeconds")}
          </div>
        </div>
        <div className="space-y-2">
          <Label htmlFor="cluster-enabled">连接状态</Label>
          <Select
            value={values.enabled ? "enabled" : "disabled"}
            onValueChange={(value) => setValue("enabled", value === "enabled")}
          >
            <SelectTrigger id="cluster-enabled" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="enabled">启用</SelectItem>
              <SelectItem value="disabled">停用</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </fieldset>
      <div className="flex gap-2 rounded-md border bg-primary/5 p-3 text-2xs leading-6 text-muted-foreground">
        <ShieldCheck className="mt-1 size-4 shrink-0" />
        <p>
          连接凭据由服务端管理。保存后可检测 REST 服务和 Slurm 控制器的连通性。
        </p>
      </div>
      {error && (
        <p
          role="alert"
          className="rounded-md border border-destructive/20 p-3 text-xs text-destructive"
        >
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2 border-t pt-4">
        <Button
          type="button"
          variant="outline"
          disabled={saving}
          onClick={onCancel}
        >
          取消
        </Button>
        <Button type="submit" disabled={saving || !profiles.length}>
          {saving && <LoaderCircle className="animate-spin" />}保存配置
        </Button>
      </div>
    </form>
  )
}
