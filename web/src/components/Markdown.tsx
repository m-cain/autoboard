import { lazy, Suspense } from "react";

const MarkdownRenderer = lazy(async () => {
  const [{ default: ReactMarkdown }, { default: rehypeSanitize }] =
    await Promise.all([import("react-markdown"), import("rehype-sanitize")]);

  return {
    default: ({ children }: { readonly children: string }) => (
      <ReactMarkdown rehypePlugins={[rehypeSanitize]}>{children}</ReactMarkdown>
    ),
  };
});

export const Markdown = ({ children }: { readonly children: string }) => (
  <div className="markdown">
    <Suspense fallback={null}>
      <MarkdownRenderer>{children}</MarkdownRenderer>
    </Suspense>
  </div>
);
