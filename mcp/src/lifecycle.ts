export type Closeable = { close(): Promise<void> }

/**
 * Serializes shutdown so repeated signals cannot interrupt cleanup. The RPC
 * client is always closed, even when the MCP transport/server close fails.
 */
export const createCloseOnce = (server: Closeable, client: Closeable): (() => Promise<void>) => {
  let closing: Promise<void> | undefined

  return () => {
    closing ??= (async () => {
      let serverFailure: unknown
      try {
        await server.close()
      } catch (error) {
        serverFailure = error
      }

      try {
        await client.close()
      } catch (clientFailure) {
        if (serverFailure === undefined) throw clientFailure
      }

      if (serverFailure !== undefined) throw serverFailure
    })()
    return closing
  }
}

export const createSignalShutdown = (
  close: () => Promise<void>,
  exit: (code: number) => void,
  logError: (message: string) => void,
): (() => void) => {
  let signalled = false

  return () => {
    if (signalled) return
    signalled = true
    void close().then(
      () => exit(0),
      (error) => {
        logError(`Autoboard MCP shutdown failed: ${error instanceof Error ? error.message : String(error)}`)
        exit(1)
      },
    )
  }
}
