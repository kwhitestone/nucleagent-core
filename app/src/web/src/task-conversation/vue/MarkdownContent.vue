<script setup lang="ts">
import DOMPurify from "dompurify";
import MarkdownIt from "markdown-it";
import { computed } from "vue";

const props = defineProps<{
  content: string;
}>();

const markdown = new MarkdownIt({
  html: false,
  breaks: false,
  linkify: true,
  typographer: false,
});
markdown.renderer.rules.image = () => "";

const rendered = computed(() =>
  DOMPurify.sanitize(markdown.render(props.content), {
    USE_PROFILES: { html: true },
    FORBID_TAGS: [
      "style",
      "form",
      "input",
      "button",
      "iframe",
      "object",
      "embed",
      "img",
    ],
    FORBID_ATTR: ["style"],
  }),
);
</script>

<template>
  <div class="atc-markdown" v-html="rendered" />
</template>
