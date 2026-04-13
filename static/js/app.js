// Auto-dismiss flash
document.querySelectorAll('.flash').forEach(el => {
  setTimeout(() => {
    el.style.transition = 'opacity .5s';
    el.style.opacity = '0';
    setTimeout(() => el.remove(), 500);
  }, 4000);
});

// Post form: live preview
const titleInput = document.getElementById('title');
const previewTitle = document.getElementById('preview-title');
const previewEmoji = document.getElementById('preview-emoji');
const pubCheck = document.getElementById('published');
const pubLabel = document.getElementById('pub-label');

const emojis = ['✨','🚀','💡','🌟','📖','🎯','🔥','💎','🌈','⚡','🎨','🏆','🌿','🦋','🎭','🔮','🌸','💫','🎪','🌊'];
function getEmoji(str) {
  let s = 0;
  for (let c of str) s += c.charCodeAt(0);
  return emojis[s % emojis.length];
}

if (titleInput && previewTitle) {
  titleInput.addEventListener('input', () => {
    const v = titleInput.value || 'Your title here';
    previewTitle.textContent = v;
    previewEmoji.textContent = getEmoji(v);
  });
}

if (pubCheck && pubLabel) {
  pubCheck.addEventListener('change', () => {
    pubLabel.textContent = pubCheck.checked ? 'Published' : 'Draft';
  });
}

// Editor toolbar helpers
function getTA() { return document.getElementById('content'); }
function wrap(before, after) {
  const ta = getTA(); if (!ta) return;
  const s = ta.selectionStart, e = ta.selectionEnd;
  const sel = ta.value.substring(s, e);
  ta.value = ta.value.substring(0, s) + before + sel + after + ta.value.substring(e);
  ta.focus(); ta.selectionStart = s + before.length; ta.selectionEnd = e + before.length;
}
function insertLine(prefix) {
  const ta = getTA(); if (!ta) return;
  const s = ta.selectionStart;
  const before = ta.value.substring(0, s);
  const lineStart = before.lastIndexOf('\n') + 1;
  ta.value = ta.value.substring(0, lineStart) + prefix + ta.value.substring(lineStart);
  ta.focus(); ta.selectionStart = ta.selectionEnd = lineStart + prefix.length;
}
