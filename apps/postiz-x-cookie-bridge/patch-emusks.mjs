import fs from "node:fs";

const unsafeReturn = "return { media_id: mediaId, ...finalizeData };";
const safeReturn = "return { ...finalizeData, media_id: mediaId };";

export function patchMediaSource(source) {
  const occurrences = source.split(unsafeReturn).length - 1;
  if (occurrences !== 1) {
    throw new Error(
      `expected exactly one unsafe media ID return, found ${occurrences}`,
    );
  }

  return source.replace(unsafeReturn, safeReturn);
}

if (import.meta.main) {
  const path =
    process.argv[2] ??
    "/opt/crosspost/node_modules/emusks/src/helpers/media.js";
  const source = fs.readFileSync(path, "utf8");
  fs.writeFileSync(path, patchMediaSource(source));
}
