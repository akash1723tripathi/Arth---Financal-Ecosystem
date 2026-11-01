import { Feather } from "@expo/vector-icons";
import { useState } from "react";
import {
  ActivityIndicator,
  FlatList,
  KeyboardAvoidingView,
  Platform,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";

type ChatMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
};

const SUGGESTED_PROMPTS = [
  "How much did I spend on food this month?",
  "What's my biggest expense this week?",
  "Am I over budget anywhere?",
];

const INITIAL_MESSAGES: ChatMessage[] = [
  {
    id: "welcome",
    role: "assistant",
    content:
      "Hi! Ask me anything about your spending or budgets in last 30 days.",
  },
];

function MessageBubble({ message }: { message: ChatMessage }) {
  const isUser = message.role === "user";
  return (
    <View className={`mb-3 max-w-[85%] ${isUser ? "self-end" : "self-start"}`}>
      <View
        className={`rounded-2xl px-3.5 py-2.5 ${
          isUser ? "bg-brand-bg" : "bg-white border border-[#E8E6DF]"
        }`}
      >
        <Text className={`text-sm ${isUser ? "text-white" : "text-brand-bg"}`}>
          {message.content}
        </Text>
      </View>
    </View>
  );
}

/**
 * The AI assistant has no server endpoint yet (post-MVP; LLM provider D-OPEN-1, `POST /v1/assistant/messages`
 * is not in the spec). The tab is hidden in release builds; in development builds the chat UI stays visible,
 * clearly marked, and answers with a placeholder. No client-side AI key, no data leaves the app.
 */
const DEV_REPLY =
  "DEV preview: the assistant isn't connected yet. It needs a server endpoint (post-MVP), so this tab is hidden in release builds.";

export default function AssistantScreen() {
  const [messages, setMessages] = useState<ChatMessage[]>(INITIAL_MESSAGES);
  const [input, setInput] = useState("");
  const sending = false;

  const sendMessage = (text: string) => {
    if (!text.trim()) return;
    const now = Date.now();
    setMessages((prev) => [
      ...prev,
      { id: `${now}`, role: "user", content: text },
      { id: `${now + 1}`, role: "assistant", content: DEV_REPLY },
    ]);
    setInput("");
  };

  if (!__DEV__) {
    return (
      <SafeAreaView className="flex-1 bg-brand-body items-center justify-center px-10" edges={["top"]}>
        <Feather name="cpu" size={32} color="#BDC3C7" />
        <Text className="text-brand-text-muted text-sm mt-3 text-center">
          The assistant is coming soon.
        </Text>
      </SafeAreaView>
    );
  }

  return (
    <SafeAreaView className="flex-1 bg-brand-body" edges={["top"]}>
      <View className="px-5 pt-3 pb-2">
        <Text className="text-brand-bg text-xl font-semibold">Assistant</Text>
        <Text className="text-brand-coral text-[11px] mt-0.5">
          DEV ONLY · not connected (no server endpoint yet)
        </Text>
      </View>

      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : undefined}
        keyboardVerticalOffset={Platform.OS === "ios" ? -70 : 0}
        className="flex-1"
      >
        <FlatList
          data={messages}
          keyExtractor={(item) => item.id}
          renderItem={({ item }) => <MessageBubble message={item} />}
          contentContainerStyle={{
            paddingHorizontal: 20,
            paddingTop: 8,
            paddingBottom: 12,
          }}
          ListFooterComponent={
            sending ? (
              <View className="self-start mb-3 bg-white border border-[#E8E6DF] rounded-2xl px-3.5 py-2.5">
                <ActivityIndicator size="small" color="#4A9EFF" />
              </View>
            ) : null
          }
        />

        {messages.length <= 1 && (
          <View className="px-5 pb-2 gap-2">
            {SUGGESTED_PROMPTS.map((prompt) => (
              <TouchableOpacity
                key={prompt}
                onPress={() => sendMessage(prompt)}
                className="bg-white rounded-xl border border-[#E8E6DF] px-3.5 py-2.5 self-start"
              >
                <Text className="text-brand-text-secondary text-xs">
                  {prompt}
                </Text>
              </TouchableOpacity>
            ))}
          </View>
        )}

        {/* fixed padding to clear the native tab bar - simplest fix, adjust the 90 if it still overlaps on your device */}
        <View
          className="flex-row items-center gap-2 px-5 pt-2"
          style={{ paddingBottom: 90 }}
        >
          <TextInput
            value={input}
            onChangeText={setInput}
            placeholder="Ask about your money..."
            placeholderTextColor="#8A8D96"
            editable={!sending}
            className="flex-1 bg-white border border-[#E8E6DF] rounded-full px-4 py-3 text-sm text-brand-bg"
            onSubmitEditing={() => sendMessage(input)}
            returnKeyType="send"
          />
          <TouchableOpacity
            onPress={() => sendMessage(input)}
            disabled={sending}
            className="w-11 h-11 rounded-full bg-brand-bg items-center justify-center"
            style={{ opacity: sending ? 0.6 : 1 }}
          >
            <Feather name="arrow-up" size={18} color="#fff" />
          </TouchableOpacity>
        </View>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}
