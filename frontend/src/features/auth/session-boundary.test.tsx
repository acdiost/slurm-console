import { act, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router"
import { pb } from "@/services/pocketbase"
import { logout, useAuth } from "@/stores/auth"
import { RequireSession, SessionLifecycle } from "./session-boundary"

afterEach(() => {
  logout()
  vi.useRealTimers()
})
it("匿名用户无法直接打开受保护页面", async () => {
  useAuth.setState({ status: "anonymous", user: null, message: "" })
  render(
    <MemoryRouter initialEntries={["/private"]}>
      <Routes>
        <Route path="/login" element={<div>需要登录</div>} />
        <Route element={<RequireSession />}>
          <Route path="/private" element={<div>private content</div>} />
        </Route>
      </Routes>
    </MemoryRouter>
  )
  expect(await screen.findByText("需要登录")).toBeVisible()
  expect(screen.queryByText("private content")).not.toBeInTheDocument()
})
it("令牌到期后自动退出并清除缓存", () => {
  vi.useFakeTimers()
  const token = `e30.${btoa(JSON.stringify({ exp: Math.floor(Date.now() / 1000) + 1, type: "auth" }))}.fixture`
  pb.authStore.save(token, {
    id: "fixture",
    collectionName: "users",
    collectionId: "users",
  })
  useAuth.setState({ status: "authenticated", user: null, message: "" })
  render(<SessionLifecycle />)
  act(() => vi.advanceTimersByTime(1100))
  expect(useAuth.getState().status).toBe("anonymous")
  expect(pb.authStore.token).toBe("")
})
