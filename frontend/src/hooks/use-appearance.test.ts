import { act, renderHook } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"
import { useAppearance } from "./use-appearance"
import { usePreferences } from "@/stores/preferences"

beforeEach(() => {
  usePreferences.setState({
    theme: "system",
    accent: "blue",
    density: "standard",
  })
  document.documentElement.classList.remove("dark")
})

it("跟随系统变化，并在卸载时解除监听", () => {
  let onChange = () => {}
  const media = {
    matches: false,
    addEventListener: vi.fn((_event, callback) => {
      onChange = callback
    }),
    removeEventListener: vi.fn(),
  }
  vi.mocked(window.matchMedia).mockReturnValue(
    media as unknown as MediaQueryList
  )
  const { unmount } = renderHook(useAppearance)
  expect(document.documentElement).not.toHaveClass("dark")
  act(() => {
    media.matches = true
    onChange()
  })
  expect(document.documentElement).toHaveClass("dark")
  unmount()
  expect(media.removeEventListener).toHaveBeenCalledWith("change", onChange)
})

it("显式主题覆盖系统偏好，配色写入根元素", () => {
  vi.mocked(window.matchMedia).mockReturnValue({
    matches: true,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  } as unknown as MediaQueryList)
  renderHook(useAppearance)
  act(() => {
    usePreferences.getState().setTheme("light")
    usePreferences.getState().setAccent("red")
  })
  expect(document.documentElement).not.toHaveClass("dark")
  expect(document.documentElement.style.colorScheme).toBe("light")
  expect(document.documentElement).toHaveAttribute("data-accent", "red")
})
