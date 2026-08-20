<script setup lang="ts">
import { Add, Close } from "@vicons/ionicons5";
import { NButton, NIcon, NInput } from "naive-ui";

interface Props {
  modelValue: string[];
  placeholder?: string;
  addText?: string;
  size?: "small" | "medium" | "large";
}

interface Emits {
  (e: "update:modelValue", value: string[]): void;
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: () => [],
  placeholder: "",
  addText: "",
  size: "small",
});

const emit = defineEmits<Emits>();

function updateItem(index: number, value: string) {
  const values = [...(props.modelValue ?? [])];
  values[index] = value;
  emit("update:modelValue", values);
}

function addItem() {
  emit("update:modelValue", [...(props.modelValue ?? []), ""]);
}

function removeItem(index: number) {
  const values = [...(props.modelValue ?? [])];
  values.splice(index, 1);
  emit("update:modelValue", values);
}
</script>

<template>
  <div class="string-list-input">
    <div v-for="(item, index) in modelValue" :key="index" class="string-list-item">
      <n-input
        :value="item"
        :size="size"
        :placeholder="placeholder"
        clearable
        @update:value="value => updateItem(index, value)"
      />
      <n-button
        quaternary
        circle
        type="error"
        size="small"
        :aria-label="`Remove item ${index + 1}`"
        @click="removeItem(index)"
      >
        <template #icon>
          <n-icon :component="Close" />
        </template>
      </n-button>
    </div>
    <n-button dashed size="small" class="string-list-add" @click="addItem">
      <template #icon>
        <n-icon :component="Add" />
      </template>
      {{ addText }}
    </n-button>
  </div>
</template>

<style scoped>
.string-list-input {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}

.string-list-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.string-list-item :deep(.n-input) {
  flex: 1;
}

.string-list-add {
  align-self: flex-start;
}
</style>
