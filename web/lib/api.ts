// 统一 API 客户端。后端固定返回 { code, data, msg }, 这里把非 200 业务码转成异常,
// 让调用方只处理 data, 错误统一走 TanStack Query 的 error 分支。

export const CODE_NEED_CONFIRM = 4090;
// 未登录。业务码与 HTTP 状态一致, 由 AuthGate 接住并切回登录页, 不当成普通接口失败弹 toast
export const CODE_NEED_LOGIN = 401;

export class ApiError extends Error {
  code: number;

  constructor(code: number, message: string) {
    super(message);
    this.code = code;
    this.name = "ApiError";
  }
}

interface Envelope<T> {
  code: number;
  data: T;
  msg: string;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
  });

  let body: Envelope<T>;
  try {
    body = await resp.json();
  } catch {
    throw new ApiError(resp.status, `服务端返回了非 JSON 响应 (HTTP ${resp.status})`);
  }
  if (body.code !== 200) {
    throw new ApiError(body.code, body.msg || `请求失败 (${body.code})`);
  }
  return body.data;
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, payload?: unknown) =>
    request<T>(path, { method: "POST", body: JSON.stringify(payload ?? {}) }),
  del: <T>(path: string) => request<T>(path, { method: "DELETE" }),
};

// lastMessage 用于把后端的 msg 展示成 toast; 走单独函数是因为正常路径只关心 data。
export async function postWithMessage<T>(
  path: string,
  payload?: unknown,
): Promise<{ data: T; msg: string }> {
  const resp = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload ?? {}),
  });
  const body: Envelope<T> = await resp.json();
  if (body.code !== 200) {
    throw new ApiError(body.code, body.msg || `请求失败 (${body.code})`);
  }
  return { data: body.data, msg: body.msg };
}
