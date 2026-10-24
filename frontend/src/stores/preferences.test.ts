import { expect, it } from "vitest"
import { sanitizePreferences, usePreferences } from "./preferences"

it("安全恢复本地偏好，丢弃损坏值及无关数据", () => {
  expect(sanitizePreferences(null)).toEqual({
    theme: "system",
    accent: "blue",
    density: "standard",
  })
  expect(
    sanitizePreferences({
      theme: "invalid",
      accent: "green",
      density: 8,
      token: "discard",
    })
  ).toEqual({ theme: "system", accent: "blue", density: "standard" })
})
it("持久化选择的主题与密度", () => {
  usePreferences.getState().setTheme("dark")
  usePreferences.getState().setDensity("compact")
  const saved = JSON.parse(localStorage.getItem("slurm-console-preferences")!)
  expect(saved.state).toEqual({
    theme: "dark",
    accent: "blue",
    density: "compact",
  })
  expect(Object.keys(saved.state)).toHaveLength(3)
})
