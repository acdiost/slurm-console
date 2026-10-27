import { expect, it } from "vitest"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { ClusterNavLink, scopedNavigation } from "./cluster-navigation"
it("跨业务导航保留集群和候选位置，不携带作业、节点、确认或筛选", () => {
  const value = scopedNavigation(
    "/nodes",
    "/jobs",
    "?cluster=beta&clusterPage=2&clusterQ=gpu&job=42&operation=proof&q=training&page=4"
  )
  expect(value).toBe("/nodes?cluster=beta&clusterPage=2&clusterQ=gpu")
})
it("审计的历史集群过滤器与全局工作台不被当作操作上下文", () => {
  expect(scopedNavigation("/jobs", "/audits", "?cluster=deleted")).toBe("/jobs")
  expect(scopedNavigation("/fleet", "/nodes", "?cluster=beta")).toBe("/fleet")
})
it("实际导航链接带上当前集群", () => {
  render(
    <MemoryRouter initialEntries={["/nodes?cluster=beta&node=node1"]}>
      <ClusterNavLink to="/jobs">作业管理</ClusterNavLink>
    </MemoryRouter>
  )
  expect(screen.getByRole("link", { name: "作业管理" })).toHaveAttribute(
    "href",
    "/jobs?cluster=beta"
  )
})

it("Linux 身份查询与其他业务页共享集群但不共享用户名详情", () => {
  expect(
    scopedNavigation("/linux-users", "/nodes", "?cluster=beta&node=node1")
  ).toBe("/linux-users?cluster=beta")
  expect(
    scopedNavigation(
      "/jobs",
      "/linux-users",
      "?cluster=beta&username=worker&source=local"
    )
  ).toBe("/jobs?cluster=beta")
})
