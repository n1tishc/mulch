#!/usr/bin/env node

import { mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import { basename, extname, join } from "node:path";

const docs = "docs";
const escape = (value) => value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");

function inline(text) {
  let value = escape(text);
  value = value.replace(/`([^`]+)`/g, "<code>$1</code>");
  value = value.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  value = value.replace(/\[([^\]]+)\]\(([^\s)]+)(?:\s+\"[^\"]*\")?\)/g, (_, label, url) => {
    const href = url.endsWith(".md") && !url.includes("/") ? `${url.slice(0, -3)}.html` : url;
    return `<a href="${href}">${label}</a>`;
  });
  return value;
}

function isTableLine(line) {
  return /^\|.*\|\s*$/.test(line);
}

function table(lines) {
  const cells = (line) => line.trim().slice(1, -1).split("|").map((cell) => inline(cell.trim()));
  const header = cells(lines[0]);
  const rows = lines.slice(2).map(cells);
  return `<table><thead><tr>${header.map((cell) => `<th>${cell}</th>`).join("")}</tr></thead><tbody>${rows.map((row) => `<tr>${row.map((cell) => `<td>${cell}</td>`).join("")}</tr>`).join("")}</tbody></table>`;
}

function markdown(source) {
  const lines = source.replaceAll("\r\n", "\n").split("\n");
  const output = [];
  let index = 0;
  while (index < lines.length) {
    const line = lines[index];
    if (!line.trim()) { index += 1; continue; }
    if (line.startsWith("```")) {
      const language = line.slice(3).trim();
      const code = [];
      index += 1;
      while (index < lines.length && !lines[index].startsWith("```")) code.push(lines[index++]);
      index += 1;
      output.push(`<pre><code class="language-${escape(language)}">${escape(code.join("\n"))}</code></pre>`);
      continue;
    }
    const heading = /^(#{1,6})\s+(.+)$/.exec(line);
    if (heading) {
      const level = heading[1].length;
      const id = heading[2].toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, "");
      output.push(`<h${level} id="${id}">${inline(heading[2])}</h${level}>`);
      index += 1;
      continue;
    }
    if (/^---+$/.test(line)) { output.push("<hr>"); index += 1; continue; }
    if (isTableLine(line) && /^\|?\s*:?-{3,}/.test(lines[index + 1] || "")) {
      const tableLines = [];
      while (index < lines.length && isTableLine(lines[index])) tableLines.push(lines[index++]);
      output.push(table(tableLines));
      continue;
    }
    const list = /^([-*+]|\d+\.)\s+(.+)$/.exec(line);
    if (list) {
      const ordered = /\d+\./.test(list[1]);
      const tag = ordered ? "ol" : "ul";
      const items = [];
      while (index < lines.length) {
        const item = /^([-*+]|\d+\.)\s+(.+)$/.exec(lines[index]);
        if (!item || (/\d+\./.test(item[1]) !== ordered)) break;
        items.push(`<li>${inline(item[2])}</li>`);
        index += 1;
      }
      output.push(`<${tag}>${items.join("")}</${tag}>`);
      continue;
    }
    const paragraph = [line];
    index += 1;
    while (index < lines.length && lines[index].trim() && !lines[index].startsWith("```") && !/^(#{1,6})\s|^---+$|^([-*+]|\d+\.)\s+/.test(lines[index]) && !isTableLine(lines[index])) paragraph.push(lines[index++]);
    output.push(`<p>${inline(paragraph.join(" "))}</p>`);
  }
  return output.join("\n");
}

const style = `
  :root { color-scheme: light dark; font-family: Inter, ui-sans-serif, system-ui, sans-serif; }
  body { max-width: 900px; margin: 0 auto; padding: 3rem 1.5rem 5rem; line-height: 1.6; color: #202124; background: #fff; }
  h1, h2, h3 { line-height: 1.2; margin-top: 2.25rem; } h1 { font-size: 2.3rem; } h2 { border-bottom: 1px solid #d0d7de; padding-bottom: .35rem; }
  a { color: #0969da; } code { padding: .12rem .32rem; border-radius: .3rem; background: #eef1f4; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  pre { overflow: auto; padding: 1rem; border-radius: .5rem; background: #f6f8fa; } pre code { padding: 0; background: none; }
  table { width: 100%; border-collapse: collapse; display: block; overflow-x: auto; } th, td { border: 1px solid #d0d7de; padding: .55rem .7rem; text-align: left; vertical-align: top; } th { background: #f6f8fa; }
  li + li { margin-top: .3rem; } hr { border: 0; border-top: 1px solid #d0d7de; margin: 2rem 0; }
  @media (prefers-color-scheme: dark) { body { color: #e6edf3; background: #0d1117; } a { color: #58a6ff; } h2, th, td { border-color: #30363d; } code, pre, th { background: #161b22; } }
`;

await mkdir(docs, { recursive: true });
const markdownFiles = (await readdir(docs)).filter((name) => extname(name) === ".md");
for (const file of markdownFiles) {
  const source = await readFile(join(docs, file), "utf8");
  const title = source.match(/^#\s+(.+)$/m)?.[1] ?? basename(file, ".md");
  const html = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>${escape(title)} · Mulch</title><style>${style}</style></head><body><main>${markdown(source)}</main></body></html>\n`;
  await writeFile(join(docs, `${basename(file, ".md")}.html`), html);
}

console.log(`Generated ${markdownFiles.length} HTML documents in ${docs}/`);
