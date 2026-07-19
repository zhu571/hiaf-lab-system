<template>
  <RouterView v-if="$route.meta.public" />
  <AppLayout v-else />
</template>

<script setup lang="ts">
import AppLayout from './components/AppLayout.vue'
</script>
