// Downloads ggml-base-q5_1.bin into paratech/dist/cache/e2e/ (size and SHA-256 from desktop/internal/app/catalog.json) and
// leaves a verified file there. The settings pack does the same by itself in its setup; run this by hand to fetch the
// model ahead of a run: `npm --prefix <abs>/paratech/e2e run fetch-model`. Network: the source_url of the catalog only.
import { ensureModel } from "../src/fixtures/model.js";

try {
  const { file, downloaded } = await ensureModel();
  console.log(`[fetch-model] ${downloaded ? "готово" : "ничего загружать не нужно"}: ${file}`);
} catch (err) {
  console.error(`[fetch-model] ОШИБКА: ${err instanceof Error ? err.message : String(err)}`);
  process.exitCode = 1;
}
