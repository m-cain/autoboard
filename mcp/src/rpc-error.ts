export class RpcError extends Error {
  readonly code: number;
  readonly data: unknown;

  constructor(code: number, message: string, data: unknown) {
    super(message);
    this.name = "RpcError";
    this.code = code;
    this.data = data;
  }
}

/** A write may have reached the server before its response connection died. */
export class IndeterminateWriteError extends Error {
  constructor(method: string) {
    super(
      `The result of ${method} is indeterminate because the RPC connection closed`,
    );
    this.name = "IndeterminateWriteError";
  }
}

export class RpcProtocolError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "RpcProtocolError";
  }
}

export class RpcConnectionError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "RpcConnectionError";
  }
}
