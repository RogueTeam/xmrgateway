// Keeps the OpenAPI version in step with package.json on release.
const patterns = [
  /(\/\/ @version\s+)(\S+)()/,
  /("version": ")([^"]+)(")/,
  /(\n  version: )(\S+)()/,
];

const pattern = (contents) => {
  const found = patterns.find((p) => p.test(contents));
  if (!found) throw new Error("no version field found");
  return found;
};

module.exports.readVersion = (contents) => contents.match(pattern(contents))[2];

module.exports.writeVersion = (contents, version) =>
  contents.replace(pattern(contents), (_, before, _old, after) => before + version + after);
