import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router"
import { logout, useAuth } from "@/stores/auth"
import { LoginPage } from "./login-page"

afterEach(() => {
  logout()
  vi.unstubAllGlobals()
})
it("校验输入并在拒绝登录后清空密码、保留邮箱", async () => {
  logout()
  useAuth.setState({ status: "anonymous", user: null, message: "" })
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify({ code: 400, message: "Failed" }), {
      status: 400,
    })
  )
  vi.stubGlobal("fetch", fetchMock)
  const user = userEvent.setup()
  render(
    <MemoryRouter>
      <LoginPage />
    </MemoryRouter>
  )
  await user.click(screen.getByRole("button", { name: "登录" }))
  expect(await screen.findByText("请输入有效的邮箱地址")).toBeVisible()
  expect(fetchMock).not.toHaveBeenCalled()
  await user.type(
    screen.getByLabelText("邮箱", { exact: true }),
    "fixture@example.com"
  )
  await user.type(
    screen.getByLabelText("密码", { exact: true }),
    "fixture-password"
  )
  await user.click(screen.getByRole("button", { name: "显示密码" }))
  expect(screen.getByLabelText("密码", { exact: true })).toHaveAttribute(
    "type",
    "text"
  )
  await user.click(screen.getByRole("button", { name: "登录" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("邮箱或密码不正确")
  expect(screen.getByLabelText("密码", { exact: true })).toHaveValue("")
  expect(screen.getByLabelText("邮箱", { exact: true })).toHaveValue(
    "fixture@example.com"
  )
})
