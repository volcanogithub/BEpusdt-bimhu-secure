import assert from "node:assert/strict";
import { test } from "node:test";
import { build } from "esbuild";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

// Exercise the actual Axios interceptors without a browser or network requests.
test("B25 login success, invalid credentials and cooldown responses", async () => {
  const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
  const directory = await mkdtemp(join(root, ".b25-test-"));
  const previousMessages = globalThis.__b25Messages;
  const previousStorage = globalThis.localStorage;
  globalThis.__b25Messages = [];
  globalThis.localStorage = { getItem: () => null };
  try {
    const result = await build({
      entryPoints: [join(root, "src/api/index.ts")],
      bundle: true,
      format: "esm",
      platform: "node",
      external: ["axios"],
      write: false,
      plugins: [{
        name: "ui-stubs",
        setup(builder) {
          builder.onResolve({ filter: /^(@arco-design\/web-vue|@\/store\/)/ }, args => ({ path: args.path, namespace: "ui-stubs" }));
          builder.onLoad({ filter: /.*/, namespace: "ui-stubs" }, args => ({
            contents: args.path === "@arco-design/web-vue"
              ? "export const Message = { error: message => globalThis.__b25Messages.push(message) };"
              : args.path.endsWith("user-info")
                ? "export const useUserInfoStore = () => ({ logOut() {} });"
                : "export default {};"
          }));
        }
      }]
    });
    const modulePath = join(directory, "api.mjs");
    await writeFile(modulePath, result.outputFiles[0].text);
    const { default: service } = await import(pathToFileURL(modulePath).href);
    const success = await service.post("/api/auth/login", {}, {
      adapter: async config => ({ status: 200, data: { code: 200, data: { token: "test-token" } }, config, headers: {} })
    });
    assert.equal(success.data.token, "test-token");
    for (const status of [401, 429]) {
      const data = { code: status, msg: "invalid credentials" };
      await assert.rejects(service.post("/api/auth/login", {}, {
        adapter: async config => { throw { config, response: { status, data } }; }
      }), error => error === data);
    }
    assert.deepEqual(globalThis.__b25Messages, ["invalid credentials", "invalid credentials"]);
  } finally {
    globalThis.__b25Messages = previousMessages;
    if (previousStorage === undefined) delete globalThis.localStorage;
    else globalThis.localStorage = previousStorage;
    await rm(directory, { recursive: true, force: true });
  }
});
