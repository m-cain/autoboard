import ReactMarkdown from "react-markdown"
import rehypeSanitize from "rehype-sanitize"

export const Markdown = ({ children }: { readonly children: string }) => (
  <div className="markdown">
    <ReactMarkdown rehypePlugins={[rehypeSanitize]}>{children}</ReactMarkdown>
  </div>
)
