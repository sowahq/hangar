import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import mdx from "@astrojs/mdx";

export default defineConfig({
  site: "https://hangar.mth.lc",
  integrations: [
    starlight({
      title: "Hangar",
      description:
        "Self-hosted object storage in Go. Content-addressed, S3-compatible.",
      social: {
        github: "https://github.com/sowahq/hangar",
      },
      editLink: {
        baseUrl: "https://github.com/sowahq/hangar/edit/main/website/",
      },
      lastUpdated: true,
      pagination: true,
      sidebar: [
        { label: "Home", link: "/" },
        { label: "Getting Started", link: "/getting-started/" },
        { label: "Configuration", link: "/configuration/" },
        {
          label: "API",
          items: [
            { label: "HTTP API", link: "/api/http/" },
            { label: "S3 API", link: "/api/s3/" },
          ],
        },
        { label: "Encryption (SSE)", link: "/sse/" },
        { label: "Architecture", link: "/architecture/" },
        { label: "Roadmap", link: "/roadmap/" },
      ],
      customCss: ["./src/styles/custom.css"],
      components: {},
      head: [
        {
          tag: "meta",
          attrs: { name: "theme-color", content: "#6366f1" },
        },
      ],
      credits: false,
    }),
    mdx(),
  ],
});
