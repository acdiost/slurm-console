import { beforeEach, describe, expect, it, vi } from "vitest"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { toast } from "sonner"
import { JobTablePreview } from "./job-table-preview"
import { SettingsFormPreview } from "./settings-form-preview"
import { OperationConfirmDialog } from "@/components/slurm/operation-confirm-dialog"
import { usePreferences } from "@/stores/preferences"

vi.mock("sonner", () => ({ toast: { success: vi.fn() } }))
beforeEach(() =>
  usePreferences.setState({
    theme: "system",
    accent: "blue",
    density: "standard",
  })
)
describe("设计系统交互", () => {
  it("搜索显示空状态，清除后恢复示例；密度选择生效", async () => {
    const user = userEvent.setup()
    render(<JobTablePreview />)
    await user.type(
      screen.getByRole("textbox", { name: "搜索示例作业" }),
      "no-such-job"
    )
    expect(screen.getByText("没有匹配的示例作业")).toBeVisible()
    await user.click(screen.getByRole("button", { name: "清除搜索" }))
    expect(screen.getByText("llama3-finetune")).toBeVisible()
    await user.click(screen.getByRole("radio", { name: "紧凑密度" }))
    expect(screen.getByRole("table")).toHaveAttribute("data-density", "compact")
  })
  it("按 CPU 排序可改变记录顺序", async () => {
    const user = userEvent.setup()
    render(<JobTablePreview />)
    await user.click(screen.getByRole("button", { name: "按CPU排序" }))
    const rows = within(screen.getByRole("table")).getAllByRole("row")
    expect(within(rows[1]).getByText("128")).toBeVisible()
  })
  it("拒绝空输入，并验证合法名称", async () => {
    const user = userEvent.setup()
    render(<SettingsFormPreview />)
    await user.click(screen.getByRole("button", { name: "验证输入" }))
    expect(await screen.findByText("请输入 2–32 个字符")).toBeVisible()
    expect(toast.success).not.toHaveBeenCalled()
    await user.type(screen.getByLabelText(/集群显示名称/), "科研集群")
    await user.click(screen.getByRole("button", { name: "验证输入" }))
    expect(toast.success).toHaveBeenCalledWith(
      "校验通过：科研集群",
      expect.any(Object)
    )
    await user.click(screen.getByRole("button", { name: "重置" }))
    expect(screen.getByLabelText(/集群显示名称/)).toHaveValue("")
  })
  it("只有正确确认文本才继续，取消重开清除确认", async () => {
    const user = userEvent.setup()
    const confirm = vi.fn()
    render(<OperationConfirmDialog onConfirm={confirm} />)
    const trigger = screen.getByRole("button", { name: "操作确认示例" })
    await user.click(trigger)
    expect(screen.getByRole("button", { name: "确认演示" })).toBeDisabled()
    await user.type(screen.getByLabelText("请输入“演示”以继续"), "演示")
    await user.click(screen.getByRole("button", { name: "返回" }))
    expect(confirm).not.toHaveBeenCalled()
    await user.click(trigger)
    expect(screen.getByRole("button", { name: "确认演示" })).toBeDisabled()
    await user.type(screen.getByLabelText("请输入“演示”以继续"), "演示")
    await user.click(screen.getByRole("button", { name: "确认演示" }))
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument()
  })
})
