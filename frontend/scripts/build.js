// Build script for the RiceGuard browser console. It copies the source
// assets into dist/ and emits a build manifest consumed by the Go backend's
// static file server. No third-party dependencies are required.
import { mkdirSync, cpSync, writeFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const src = join(root, "src");
const dist = join(root, "dist");

mkdirSync(dist, { recursive: true });
for (const name of ["index.html", "main.js", "styles.css"]) {
  cpSync(join(src, name), join(dist, name));
}

writeFileSync(
  join(dist, "build.json"),
  JSON.stringify({ builtAt: new Date().toISOString(), version: "0.1.0" }, null, 2)
);

console.log(`RiceGuard console built to ${dist}`);
