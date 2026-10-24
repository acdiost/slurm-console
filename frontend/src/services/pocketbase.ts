import PocketBase, { BaseAuthStore, type RecordModel } from "pocketbase"

const storageKey = "slurm-console-session"
type SessionStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">

// Reuse the SDK's token validation and change notifications, with tab-scoped storage.
// No fallback to localStorage: an unavailable sessionStorage means memory-only auth.
export function createPlatformClient(storage?: SessionStorage) {
  const store = new BaseAuthStore()
  try {
    const saved = JSON.parse(storage?.getItem(storageKey) || "null")
    if (
      typeof saved?.token === "string" &&
      saved?.record?.collectionName === "users" &&
      typeof saved.record.id === "string"
    ) {
      store.save(saved.token, saved.record as RecordModel)
      if (!store.isValid) store.clear()
    }
    if (!store.isValid) storage?.removeItem(storageKey)
  } catch {
    try {
      storage?.removeItem(storageKey)
    } catch {
      /* storage can be disabled */
    }
  }
  store.onChange((token, record) => {
    try {
      if (token && record?.collectionName === "users")
        storage?.setItem(storageKey, JSON.stringify({ token, record }))
      else storage?.removeItem(storageKey)
    } catch {
      /* Keep the current in-memory session when storage is disabled. */
    }
  })
  return new PocketBase("/", store)
}

function browserStorage() {
  try {
    return window.sessionStorage
  } catch {
    return undefined
  }
}
export const pb = createPlatformClient(browserStorage())
