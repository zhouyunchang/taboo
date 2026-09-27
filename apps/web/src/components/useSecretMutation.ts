import { useRef } from "react";
import { api, ApiError } from "../api";

// Memory-only receipt survives retries in the same form. Never persist plaintext.
// An unknown outcome must be resolved with exactly the original request.
export default function useSecretMutation() {
  const receipt = useRef<{ signature: string; key: string } | null>(null);
  const running = useRef(false);
  return async (method: "post" | "del", path: string, body?: unknown) => {
    if (running.current) throw new Error("操作正在提交，请稍候。");
    const signature = JSON.stringify([method, path, body]);
    if (receipt.current && receipt.current.signature !== signature)
      throw new Error(
        "上次提交结果尚未确认。请恢复原始内容并重试，或关闭后刷新查看服务器版本。",
      );
    if (!receipt.current)
      receipt.current = { signature, key: crypto.randomUUID() };
    running.current = true;
    try {
      const options = { idempotencyKey: receipt.current.key };
      const result = await (method === "post"
        ? api.post(path, body, options)
        : api.del(path, options));
      receipt.current = null;
      return result;
    } catch (e) {
      // Definitive rejection permits correction. Transport and server failures
      // retain the receipt, including a potentially lost commit acknowledgement.
      if (e instanceof ApiError && e.status >= 400 && e.status < 500)
        receipt.current = null;
      throw e;
    } finally {
      running.current = false;
    }
  };
}
