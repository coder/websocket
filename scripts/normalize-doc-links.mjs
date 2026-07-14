import { readdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

const outputDir = "ci/out/static";
const basePath = "websocket";

for (const entry of await readdir(outputDir, { recursive: true })) {
  if (!entry.endsWith(".html")) {
    continue;
  }

  const path = join(outputDir, entry);
  const html = await readFile(path, "utf8");
  const normalized = html.replaceAll(
    /(?<attribute>href|src)="\/(?!websocket(?:\/|"))(?<path>[^"]*)"/g,
    '$<attribute>="/' + basePath + '/$<path>"',
  );

  await writeFile(path, normalized);
}
