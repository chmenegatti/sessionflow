import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import { Browser } from "@wailsio/runtime";

// Conteúdo de conversa nunca vira HTML cru: react-markdown ignora HTML
// (skipHtml) e sanitiza URLs. Links abrem no navegador do sistema (nunca
// dentro da janela do app) e imagens remotas não são carregadas: o app
// funciona offline e não faz requisições por conta do histórico.
const components: Components = {
  a: ({ href, children }) => {
    const safe = href && /^https?:\/\//i.test(href) ? href : undefined;
    return (
      <a
        href={safe}
        title={safe}
        className="text-primary underline underline-offset-2"
        onClick={(e) => {
          e.preventDefault();
          if (safe) void Browser.OpenURL(safe);
        }}
      >
        {children}
      </a>
    );
  },
  img: ({ alt, src }) => (
    <span className="rounded-md border bg-white/[0.04] px-1 text-xs text-muted-foreground" title={typeof src === "string" ? src : undefined}>
      [imagem{alt ? `: ${alt}` : ""}]
    </span>
  ),
};

const rehype = [[rehypeHighlight, { detect: false, ignoreMissing: true }]] as const;

export function Markdown({ text }: { text: string }) {
  return (
    <div className="markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={rehype as never} components={components} skipHtml>
        {text}
      </ReactMarkdown>
    </div>
  );
}

// Bloco de código com destaque, montado como cerca Markdown. A cerca é mais
// longa que qualquer sequência de crases do conteúdo.
export function Code({ code, language }: { code: string; language?: string }) {
  const longest = Math.max(2, ...(code.match(/`+/g) ?? []).map((s) => s.length));
  const fence = "`".repeat(longest + 1);
  // A linguagem vem do histórico: só caracteres de nome de linguagem, para
  // não quebrar a cerca.
  const lang = (language ?? "").replace(/[^\w+#.-]/g, "");
  return <Markdown text={`${fence}${lang}\n${code}\n${fence}`} />;
}
