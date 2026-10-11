'use strict';
// One instance per authorized media/player. Position, wall-clock watch, and coverage stay distinct.
function createVideoTracker(io, mediaId, playerId) {
  let state = 'paused', visible = false, rate = 1, position = 0, duration = 0, last = null, seq = 0;
  let ranges = [], watch = 0;
  function merge(start, end) {
    if (end <= start) return;
    const sorted = ranges.concat([[start, end]]).sort((a,b) => a[0]-b[0]), result = [];
    sorted.forEach(r => { const tail=result[result.length-1]; if (tail && r[0] <= tail[1]) tail[1]=Math.max(tail[1],r[1]); else result.push(r.slice()); });
    ranges = result.slice(0, 256);
  }
  function send(type, extra) {
    if(duration<=0)return;
    io.emit(type, Object.assign({ media_id: mediaId, player_instance_id: playerId, seq: ++seq,
      current_time_ms: Math.round(position), video_duration_ms: Math.round(duration), playback_rate: rate,
      play_state: state, video_watch_ms: Math.floor(watch), played_ranges: ranges.map(r => r.map(Math.round)) }, extra || {}));
    watch = 0;
  }
  return {
    visible(value) { visible=!!value; last=null; },
    state(value) { state=value; last=null; send('video_state'); },
    rate(value) { rate=Math.min(4, Math.max(.25, Number(value)||1)); last=null; },
    progress(seconds, total) {
      const now=io.mono(), next=Math.max(0,Number(seconds)||0)*1000;
      duration=Math.max(0, Number(total)||0)*1000;
      const delta=last==null ? 0 : now-last, advance=next-position;
      const playing=visible && state==='playing' && delta>0 && delta<=2000;
      const seek=playing && (advance < -100 || advance > delta*rate + 1000);
      if (playing && !seek && advance > 0) { watch += delta; merge(position,Math.min(next,duration || next)); }
      position=duration>0?Math.min(next,duration):next; last=now;
      send('video_progress', seek ? {quality_flags:['inferred_seek']} : {});
    },
    snapshot() { return { ranges, position, duration, unique_played_ms:ranges.reduce((n,r)=>n+r[1]-r[0],0) }; }
  };
}
module.exports={createVideoTracker};
