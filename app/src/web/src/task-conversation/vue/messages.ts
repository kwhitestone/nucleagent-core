import type { ConversationLocale, ConversationMessages } from "../core";

export const conversationMessages: Record<
  ConversationLocale,
  ConversationMessages
> = {
  "zh-CN": {
    composerPlaceholder: "输入消息，Enter 发送，Shift + Enter 换行",
    send: "发送",
    stop: "停止",
    retry: "重试",
    rerun: "重新执行",
    attach: "添加附件",
    loading: "正在加载对话…",
    loadOlder: "加载更早消息",
    jumpLatest: "回到最新",
    reconnecting: "正在重新连接…",
    disconnected: "连接已断开",
    empty: "开始一段新对话",
    process: "执行过程",
    error: "操作失败，请重试",
    share: "分享",
    helpful: "有帮助",
    notHelpful: "没有帮助",
    diagnostics: "诊断信息",
    removeAttachment: "移除附件",
  },
  "en-US": {
    composerPlaceholder: "Message, Enter to send, Shift + Enter for a new line",
    send: "Send",
    stop: "Stop",
    retry: "Retry",
    rerun: "Run again",
    attach: "Add attachment",
    loading: "Loading conversation…",
    loadOlder: "Load earlier messages",
    jumpLatest: "Jump to latest",
    reconnecting: "Reconnecting…",
    disconnected: "Connection lost",
    empty: "Start a new conversation",
    process: "Execution process",
    error: "Something went wrong. Please retry.",
    share: "Share",
    helpful: "Helpful",
    notHelpful: "Not helpful",
    diagnostics: "Diagnostics",
    removeAttachment: "Remove attachment",
  },
};

export const resolveMessages = (
  locale: ConversationLocale,
  overrides?: Partial<ConversationMessages>,
): ConversationMessages => ({ ...conversationMessages[locale], ...overrides });
