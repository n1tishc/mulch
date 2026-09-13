import { readFileSync, statSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { resolve, relative } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const directory = resolve(root, "internal/server/ui_dist");
const index = resolve(directory, "index.html");
const html = readFileSync(index, "utf8");
const assets = [...html.matchAll(/(?:src|href)="\/(assets\/[^"]+)"/g)].map(match => match[1]);
if (!assets.some(path => path.endsWith(".js")) || !assets.some(path => path.endsWith(".css"))) {
  throw new Error("Embedded index must reference a JavaScript bundle and stylesheet");
}
for (const path of [index, ...assets.map(asset => resolve(directory, asset))]) {
  if (!path.startsWith(directory + "/") || !statSync(path).isFile()) {
    throw new Error("Missing or invalid embedded asset: " + path);
  }
  if (process.argv.includes("--tracked")) {
    execFileSync("git", ["ls-files", "--error-unmatch", "--", relative(root, path)], {
      cwd: root, stdio: "pipe",
    });
  }
}
console.log(`Embedded web build verified: ${assets.length} referenced assets${process.argv.includes("--tracked") ? " (tracked)" : ""}.`);
