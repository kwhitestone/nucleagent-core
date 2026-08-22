import type {
  CommandResult,
  ConversationAdapter,
  ConversationAttachment,
  ConversationCommand,
  ConversationEvent,
  ConversationItem,
  ConversationPage,
  ConversationSnapshot,
  OlderPageRequest,
  SnapshotRequest,
  SubscribeRequest,
  UploadOptions,
} from "@/task-conversation/core";
import type {
  Message,
  MessageAttachment,

  MessageType,
} from "@/api/types";
import { streamMessages, getMessages, followUp, cancelConversation } from "@/api/conversation";
import { uploadFile } from "@/api/storage";

/**
 * 把 nucleagent 后端（REST + 全量 Message SSE）桥接到 task-conversation
 * 组件的 ConversationAdapter（V2 协议、增量事件）。
 *
 * 两套模型的差异与映射策略：
 *
 * 1. 消息 → ConversationItem
 *    后端推**完整 Message 行**（message-created/-updated/-deleted），没有
 *    stream.append 增量。所以 subscribe 一律产出 item.upsert：流式行每次
 *    整行替换，reducer 按 item.id upsert，效果等同增量。msgType=streaming
 *    的行给 status="streaming"，终态（result/error/text）给 "complete"/"failed"。
 *
 * 2. revision/seq 版本号（关键，不能偷懒）
 *    reducer.compareVersion 要求同一 stream 内事件版本严格 +1 递增，否则
 *    判 duplicate/gap 丢弃甚至触发重拉快照。本 adapter 在**订阅生命周期内**
 *    为每个 item.id 维护已发 revision：首帧 revision=1，后续每帧 +1。
 *    seq 沿用消息 id（reducer 只对同一 item 的版本做连续性比较）。
 *
 * 3. lane / userReadable / title
 *    - user / text / result → answer（主内容区）
 *    - streaming / tool_call / plan / status → process（过程区，思考气泡）
 *    注意 selectTurns 会过滤 lane=process 且 userReadable!==true 的条目，
 *    所以过程条目必须显式 userReadable: true 才能显示。
 *    title 由 senderName 映射（如 "agent.thinking" → 「思考中」）。
 *    error → system lane + 自定义 renderer。
 *
 * 4. 分页
 *    后端 messages 接口不分页（一次全量返回）。loadSnapshot 返回全部历史，
 *    hasOlder 恒为 false，loadOlder 不会被触发。
 *
 * 5. 命令
 *    send → followUp（乐观 item 由组件 controller 自己加）。
 *    stop → cancel。retry/rerun 后端没有对应接口，返回 not accepted。
 */

/**
 * 已发送未确认的乐观消息：`{conversationId}:{clientMessageId}` → content。
 *
 * 必须放模块级：controller 的订阅 generator 在页面加载时创建、持有创建时
 * adapter 实例的闭包；而 execute 每次调用走的是 props 上较新的实例
 * （视图重渲染/HMR 会重建 computed adapter）。放实例内会导致记录与消费
 * 分属两个实例、乐观条目永远无法与 SSE 真实 user 消息对账（重复气泡根因）。
 * 按 conversationId 前缀隔离，避免多对话串号。
 */
const pendingSends = new Map<string, string>();

/**
 * 本轮未终结的 thinking 消息缓存（adapter 实例内）：主答案终态时补发
 * complete 状态，驱动"思考中"动画停止。见 subscribe 里的补发逻辑。
 */
const thinkingItems = new Map<number, Message>();

/** senderName → 过程条目标题（对用户可读的中文标签）。 */
/** thinking 行标题：进行中「思考中」，结束「思考过程」——静止的历史条
 *  不能还挂着"思考中"（用户会以为还在跑）。工具调用行沿用工具名。 */
function thinkingTitle(isActive: boolean): string {
  return isActive ? "思考中" : "思考过程";
}

/** 每条后端消息固定归入同一个 turn（后端没有 turn 概念）。 */
const TURN_ID = "main";

/**
 * 主 agent 的 streaming 行（senderName=agent，无 .thinking 后缀）是**正在输出的
 * 答案正文**，必须走 answer lane —— 前端才会立即出现助手输出气泡（组件对
 * answer lane 的 streaming item 渲染 .atc-stream-text）。
 * 只有 thinking（senderName 带 .thinking）和工具调用才是 process。
 */
function mapLane(msg: Message) {
  if (msg.senderType === "user") return "answer" as const;
  switch (msg.msgType) {
    case "streaming":
      return msg.senderName.endsWith(".thinking") ? ("process" as const) : ("answer" as const);
    case "tool_call":
    case "plan":
    case "status":
      return "process" as const;
    case "error":
      return "system" as const;
    default:
      return "answer" as const;
  }
}

function mapStatus(msgType: MessageType): ConversationItem["status"] {
  switch (msgType) {
    case "streaming":
      return "streaming";
    case "error":
      return "failed";
    default:
      return "complete";
  }
}

/** 旧视图的可见性规则收窄到 kind 上：空 tool_call / 纯状态行不产出 item。 */
function shouldEmit(msg: Message): boolean {
  if (msg.msgType === "tool_call") return (msg.content || "").trim() !== "";
  // "✓ 0.3s" 之类的纯完成状态行没有信息量，折叠条目里只会堆噪音。
  if (msg.msgType === "status") return false;
  return ["text", "result", "error", "tool_call", "streaming", "plan"].includes(msg.msgType);
}

function mapAttachments(m: Message): ConversationAttachment[] | undefined {
  const raw = m.metadata?.attachments;
  if (!Array.isArray(raw)) return undefined;
  const list = raw.filter(
    (a): a is MessageAttachment =>
      typeof a === "object" && a !== null && typeof (a as MessageAttachment).fileId === "string",
  );
  if (!list.length) return undefined;
  return list.map((a) => ({
    id: a.fileId,
    name: a.name,
    mimeType: a.mimeType,
    size: a.size,
    metadata: a.kind ? { kind: a.kind } : undefined,
  }));
}

export function createConversationAdapter(conversationId: () => string): ConversationAdapter {
  const key = () => conversationId();

  /**
   * 订阅期间每个 item 已推送的 revision。reducer 要求同一 stream 的
   * 后续事件 revision 严格 +1，所以每次 upsert 递增计数。
   */
  const revisions = new Map<string, number>();
  const nextRevision = (itemId: string): number => {
    const next = (revisions.get(itemId) ?? 0) + 1;
    revisions.set(itemId, next);
    return next;
  };

  /**
   * 每个 item 的 seq 计数。规则（compareVersion 的约束）：
   *   - 新 item（无前值）：seq 必须为 1，否则判 gap；
   *   - 已有 item：seq 必须 = 前值 + 1，否则判 duplicate/gap。
   * 所以 seq 与 revision 一样按 item 独立计数，从 1 起。
   */
  const seqs = new Map<string, number>();
  const nextSeq = (itemId: string): number => {
    const next = (seqs.get(itemId) ?? 0) + 1;
    seqs.set(itemId, next);
    return next;
  };

  function toItem(msg: Message): ConversationItem {
    const lane = mapLane(msg);
    const isProcess = lane === "process";
    return {
      id: `msg-${msg.id}`,
      turnId: TURN_ID,
      streamId: `msg-${msg.id}`,
      lane,
      role: msg.senderType === "agent" ? "assistant" : msg.senderType,
      kind: msg.msgType,
      content: msg.content || "",
      status: mapStatus(msg.msgType),
      revision: 0, // 实际 revision 在事件封装时按 revisions 计数赋值
      seq: 0, // 同上，事件封装时赋全局递增 seq
      timestamp: msg.createdAt,
      // process lane 条目必须显式标记 userReadable，否则 selectTurns 会过滤掉。
      userReadable: isProcess ? true : undefined,
      title: isProcess
        ? msg.senderName.endsWith(".thinking")
          ? thinkingTitle(mapStatus(msg.msgType) === "streaming")
          : msg.senderName
        : undefined,
      attachments: mapAttachments(msg),
    };
  }

  function baseEventFields(conversationKey: string, item: ConversationItem) {
    return {
      protocolVersion: 2 as const,
      conversationKey,
      // eventId 必须每帧唯一：reducer 按 seenEventIds 去重，同 id 的第二帧
      //（流式更新）会被当重复事件丢弃。
      eventId: `${item.id}-r${item.revision}`,
      cursor: item.id,
      turnId: item.turnId,
      streamId: item.streamId,
      lane: item.lane,
      seq: item.seq,
      revision: item.revision,
      timestamp: item.timestamp,
    };
  }

  return {
    async loadSnapshot(request: SnapshotRequest): Promise<ConversationSnapshot> {
      void request;
      const list = await getMessages(key());
      // 快照条目 revision/seq 必须从 1 起（compareVersion：无前值时 >1 判 gap）。
      // seq 按数组位置递增，保持消息顺序。
      // 历史里的 streaming 思考行按 complete 处理：后端不提升思考行的终态
      // （streaming 永远留在库里），若照搬 streaming 状态，所有历史思考条
      // 都会被当成"正在进行中"（如呼吸动画常驻）。活跃与否只由订阅期间的
      // 实时更新表达。
      const items = list.filter(shouldEmit).map((m, i) => {
        const base = toItem(m);
        const finalStatus =
          m.msgType === "streaming" ? ("complete" as const) : mapStatus(m.msgType);
        return {
          ...base,
          status: finalStatus,
          // 历史思考行的标题用终态文案（"思考过程"）；工具行保留工具名。
          title:
            m.senderName.endsWith(".thinking") && mapStatus(m.msgType) === "streaming"
              ? thinkingTitle(false)
              : base.title,
          revision: 1,
          seq: i + 1,
        };
      });
      return {
        items,
        status: "idle",
        hasOlder: false,
      };
    },

    async loadOlder(request: OlderPageRequest): Promise<ConversationPage> {
      void request;
      // 后端 messages 接口不支持分页；快照已含全部历史。
      return { items: [], hasOlder: false };
    },

    async *subscribe(request: SubscribeRequest): AsyncIterable<ConversationEvent> {
      // 重新订阅（换 conversation / 重连）时重置版本计数。
      revisions.clear();
      seqs.clear();
      for await (const ev of streamMessages(key(), request.signal)) {
        if (!ev.message) continue;
        const msg = ev.message;
        if (ev.event === "message-deleted") {
          const id = `msg-${ev.id}`;
          revisions.delete(id);
          yield {
            ...baseEventFields(key(), {
              ...toItem(msg),
              id,
              revision: nextRevision(id),
              seq: nextSeq(id),
            }),
            type: "item.remove" as const,
            itemId: id,
          };
          continue;
        }
        if (!shouldEmit(msg)) continue;
        const id = `msg-${msg.id}`;
        // 记录本轮 thinking 行（senderName 带 .thinking），答案终态时补发 complete。
        if (msg.msgType === "streaming" && msg.senderName.endsWith(".thinking")) {
          thinkingItems.set(msg.id, msg);
        }
        // user 消息：按内容匹配 pending 乐观发送，补挂 clientMessageId
        // 让 reducer 替换（而非追加）乐观条目。
        let clientMessageId: string | undefined;
        if (msg.senderType === "user") {
          const prefix = `${key()}:`;
          for (const [k, content] of pendingSends) {
            if (k.startsWith(prefix) && content === msg.content) {
              clientMessageId = k.slice(prefix.length);
              pendingSends.delete(k);
              break;
            }
          }
        }
        const item = {
          ...toItem(msg),
          revision: nextRevision(id),
          seq: nextSeq(id),
          clientMessageId,
        };
        yield {
          ...baseEventFields(key(), item),
          type: "item.upsert" as const,
          item,
        };
        // 主答案终态到达 → 本轮 thinking 行补发 complete。
        // 后端只 finalize 主答案（stream_upsert），thinking 行永远停留在
        // msg_type=streaming；不补发的话思考条的"进行中"状态（如呼吸动画）
        // 永不消失。
        if (
          (msg.msgType === "result" || msg.msgType === "error") &&
          msg.senderType === "agent"
        ) {
          // thinking 行的 item id 形如 msg-<id>；从缓存补发
          for (const m of thinkingItems.values()) {
            const tid = `msg-${m.id}`;
            const done = {
              ...toItem(m),
              status: "complete" as const,
              // 标题同步换成终态文案（toItem 按消息的 msgType=streaming
              // 会算出"思考中"，补发时必须覆盖为"思考过程"）。
              title: thinkingTitle(false),
              revision: nextRevision(tid),
              seq: nextSeq(tid),
            };
            yield {
              ...baseEventFields(key(), done),
              type: "item.upsert" as const,
              item: done,
            };
          }
          thinkingItems.clear();
        }
      }
    },

    async execute(
      command: ConversationCommand,
      options: { signal: AbortSignal },
    ): Promise<CommandResult> {
      switch (command.type) {
        case "send": {
          const atts = command.attachments?.map((a) => ({ fileId: a.id, name: a.name }));
          // 必须在 await followUp 之前登记：后端在 follow-up HTTP 响应返回【前】
          // 就会向 SSE 通道 publish user 消息（svc.FollowUp 写库后立即
          // PublishCreated），若先 await 再登记，订阅循环处理 user 帧时
          // pendingSends 里还没有这条 → clientMessageId 挂不上 → 乐观条目
          // 不被替换（重复气泡）。
          const pendingKey = command.clientMessageId
            ? `${key()}:${command.clientMessageId}`
            : undefined;
          if (pendingKey) pendingSends.set(pendingKey, command.content);
          try {
            await followUp(key(), command.content, atts);
          } catch (error) {
            // 发送失败：撤销登记，否则下一条同内容消息会误吞 clientMessageId。
            if (pendingKey) pendingSends.delete(pendingKey);
            throw error;
          }
          return { accepted: true };
        }
        case "stop": {
          try {
            await cancelConversation(key());
          } catch {
            // 后端对「未在执行」返回 404 —— 视为已停止，不向用户报错。
          }
          return { accepted: true };
        }
        default:
          // retry / rerun / interaction.respond / feedback 后端暂无对应接口。
          return { accepted: false, message: "Unsupported command" };
      }
      void options;
    },

    async uploadAttachment(file: File, options: UploadOptions): Promise<ConversationAttachment> {
      void options;
      const a = await uploadFile(file);
      return { id: a.fileId, name: a.name, mimeType: a.mimeType, size: a.size };
    },
  };
}
