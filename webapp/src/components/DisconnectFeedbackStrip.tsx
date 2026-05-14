import { useState, useEffect, useRef, useCallback } from 'react';
import { Box, Rating, Chip, Typography, Button, LinearProgress } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useConnectionStore, type LastConnectionInfo } from '../stores/connection.store';
import { useVPNMachineStore } from '../stores/vpn-machine.store';
import {
  TAG_KEYS,
  type TagKey,
  submitSimpleRating,
  submitNegativeFire,
} from '../services/disconnect-feedback';

const DURATION_MS = 5000;
const COLLAPSED_HEIGHT = 56;
// Sized for chip-wrap-to-three-rows worst case (small phones + large system
// font). Natural content height is ~140 px; 240 leaves headroom so the Submit
// button is never clipped. Used as a ceiling, not an exact value.
const EXPANDED_HEIGHT = 240;

type StripPhase = 'countdown' | 'chips';

export function DisconnectFeedbackStrip() {
  const { t } = useTranslation();
  const pendingFeedback = useConnectionStore((s) => s.pendingFeedback);
  const lastConnectionInfo = useConnectionStore((s) => s.lastConnectionInfo);
  const clearPendingFeedback = useConnectionStore((s) => s.clearPendingFeedback);
  const vpnState = useVPNMachineStore((s) => s.state);

  const [phase, setPhase] = useState<StripPhase>('countdown');
  const [stars, setStars] = useState(0);
  const [tags, setTags] = useState<TagKey[]>([]);
  const [progress, setProgress] = useState(0);

  // Refs hold current values for use inside effects without invalidating deps.
  const infoRef = useRef<LastConnectionInfo | null>(null);
  const phaseRef = useRef<StripPhase>('countdown');
  const tagsRef = useRef<TagKey[]>([]);
  const starsRef = useRef(0);
  const submittedRef = useRef(false);

  phaseRef.current = phase;
  tagsRef.current = tags;
  starsRef.current = stars;

  // Stable submit + dismiss helpers (no deps → identity preserved across renders).
  const dismiss = useCallback(() => {
    clearPendingFeedback();
  }, [clearPendingFeedback]);

  const fireGood = useCallback(() => {
    if (submittedRef.current || !infoRef.current) return;
    submittedRef.current = true;
    submitSimpleRating('good', infoRef.current);
    dismiss();
  }, [dismiss]);

  const fireBadNoTags = useCallback(() => {
    if (submittedRef.current || !infoRef.current) return;
    submittedRef.current = true;
    submitSimpleRating('bad', infoRef.current);
    dismiss();
  }, [dismiss]);

  const fireBadWithTags = useCallback(() => {
    if (submittedRef.current || !infoRef.current) return;
    submittedRef.current = true;
    submitNegativeFire(infoRef.current, starsRef.current || 1, tagsRef.current);
    dismiss();
  }, [dismiss]);

  // Phase 1: pendingFeedback flips true → snapshot info, reset internal state.
  useEffect(() => {
    if (!pendingFeedback || !lastConnectionInfo) return;
    infoRef.current = lastConnectionInfo;
    submittedRef.current = false;
    setPhase('countdown');
    setStars(0);
    setTags([]);
    setProgress(0);
  }, [pendingFeedback, lastConnectionInfo]);

  // Phase 2: countdown driver — RAF with visibility pause. Restarts only on
  // (pendingFeedback, phase) change; submit helpers are stable refs.
  useEffect(() => {
    if (!pendingFeedback || phase !== 'countdown') return;
    let start = performance.now();
    let pausedAt: number | null = null;
    let rafId = 0;

    const tick = (now: number) => {
      const elapsed = now - start;
      if (elapsed >= DURATION_MS) {
        setProgress(100);
        fireGood();
        return;
      }
      setProgress((elapsed / DURATION_MS) * 100);
      rafId = requestAnimationFrame(tick);
    };
    rafId = requestAnimationFrame(tick);

    const onVisibility = () => {
      if (document.visibilityState === 'hidden') {
        if (pausedAt === null) {
          pausedAt = performance.now();
          cancelAnimationFrame(rafId);
        }
      } else if (pausedAt !== null) {
        start += performance.now() - pausedAt;
        pausedAt = null;
        rafId = requestAnimationFrame(tick);
      }
    };
    document.addEventListener('visibilitychange', onVisibility);

    return () => {
      cancelAnimationFrame(rafId);
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, [pendingFeedback, phase, fireGood]);

  // Phase 3: re-engagement detection. Any non-idle VPN state while strip is
  // open means the user reconnected (or a new connect started) — dismiss.
  // `phase` is intentionally read from phaseRef instead of state so this
  // effect only re-runs on vpnState transitions; phase changes (countdown →
  // chips) must not retrigger it. `pendingFeedback` is set to true only
  // when vpnState reaches `idle` (connection.store.ts), so this effect
  // cannot fire-on-mount before the strip is meant to be visible.
  useEffect(() => {
    if (!pendingFeedback) return;
    if (vpnState === 'idle') return;
    if (phaseRef.current === 'countdown') {
      fireGood();
    } else {
      fireBadWithTags();
    }
  }, [pendingFeedback, vpnState, fireGood, fireBadWithTags]);

  const handleStarChange = useCallback((_e: unknown, newValue: number | null) => {
    const v = newValue ?? 0;
    if (v <= 0) return;
    setStars(v);
    if (v >= 4) {
      fireGood();
    } else if (v === 3) {
      fireBadNoTags();
    } else {
      // 1-2★ → enter CHIPS, halt countdown (effect cleans up RAF on phase change)
      setPhase('chips');
    }
  }, [fireGood, fireBadNoTags]);

  const toggleTag = useCallback((key: TagKey) => {
    setTags((prev) =>
      prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key],
    );
  }, []);

  const visible = pendingFeedback && !!lastConnectionInfo;
  const expanded = visible && phase === 'chips';
  const targetHeight = !visible ? 0 : expanded ? EXPANDED_HEIGHT : COLLAPSED_HEIGHT;

  return (
    <Box
      data-testid="disconnect-feedback-strip"
      sx={{
        flexShrink: 0,
        overflow: 'hidden',
        maxHeight: `${targetHeight}px`,
        transition: 'max-height 0.25s cubic-bezier(0.4, 0, 0.2, 1)',
        backgroundColor: 'background.paper',
        borderTop: (theme) => `1px solid ${theme.palette.divider}`,
      }}
      aria-hidden={!visible}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', px: 2, py: 1, gap: 1.5 }}>
        <Typography variant="body2" sx={{ fontWeight: 500, color: 'text.primary' }}>
          {t('feedback:feedback.disconnectFeedback.title')}
        </Typography>
        <Rating
          value={stars}
          size="small"
          onChange={handleStarChange}
          sx={{ color: 'warning.main' }}
        />
      </Box>

      {phase === 'countdown' && (
        <LinearProgress
          variant="determinate"
          value={progress}
          sx={{
            height: 2,
            '& .MuiLinearProgress-bar': {
              transition: 'none',
            },
          }}
        />
      )}

      {expanded && (
        <Box sx={{ px: 2, pb: 1.5, pt: 1 }}>
          <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', mb: 1 }}>
            {t('feedback:feedback.disconnectFeedback.detailTitle')}
          </Typography>
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75, mb: 1 }}>
            {TAG_KEYS.map((key) => {
              const selected = tags.includes(key);
              return (
                <Chip
                  key={key}
                  label={t(`feedback:feedback.disconnectFeedback.tags.${key}`)}
                  size="small"
                  variant={selected ? 'filled' : 'outlined'}
                  color={selected ? 'primary' : 'default'}
                  onClick={() => toggleTag(key)}
                />
              );
            })}
          </Box>
          <Button
            variant="contained"
            size="small"
            fullWidth
            onClick={fireBadWithTags}
          >
            {t('feedback:feedback.disconnectFeedback.submit')}
          </Button>
        </Box>
      )}
    </Box>
  );
}
