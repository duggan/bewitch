// Homepage reel: plays muted and looped while on screen, with a pause button,
// and highlights the chapter matching the playback position (the chapters are
// the captions; clicking one seeks to it). Without this script the video just
// shows native controls; with "reduce motion" set it never starts by itself.
(function () {
  var video = document.getElementById('reel');
  var toggle = document.getElementById('reel-toggle');
  var list = document.getElementById('reel-chapters');
  if (!video || !toggle || !list) return;

  var buttons = Array.prototype.slice.call(list.querySelectorAll('button[data-t]'));
  var times = buttons.map(function (b) { return parseFloat(b.dataset.t); });
  var reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var userPaused = reduceMotion; // reduced motion: wait for the viewer to press play
  var active = -1;

  function setActive(i) {
    if (i === active) return;
    if (active >= 0) {
      buttons[active].classList.remove('active');
      buttons[active].removeAttribute('aria-current');
    }
    active = i;
    buttons[i].classList.add('active');
    buttons[i].setAttribute('aria-current', 'step');
  }

  function sync() {
    var t = video.currentTime, i = 0;
    while (i + 1 < times.length && t >= times[i + 1]) i++;
    setActive(i);
  }

  function showState() {
    var playing = !video.paused;
    toggle.classList.toggle('paused', !playing);
    toggle.setAttribute('aria-label', playing ? 'Pause the demo' : 'Play the demo');
  }

  function play() {
    var p = video.play();
    if (p && p.catch) p.catch(function () { /* autoplay refused: leave the poster */ });
  }

  if (!reduceMotion) {
    // Our own toggle replaces the native controls.
    video.controls = false;
    toggle.hidden = false;

    if ('IntersectionObserver' in window) {
      new IntersectionObserver(function (entries) {
        entries.forEach(function (e) {
          if (e.isIntersecting && !userPaused) play();
          else if (!e.isIntersecting) video.pause();
        });
      }, { threshold: 0.35 }).observe(video);
    } else {
      play();
    }
  }

  toggle.addEventListener('click', function () {
    if (video.paused) { userPaused = false; play(); }
    else { userPaused = true; video.pause(); }
  });

  buttons.forEach(function (b, i) {
    b.addEventListener('click', function () {
      video.currentTime = times[i];
      setActive(i);
      if (!reduceMotion || !video.paused) { userPaused = false; play(); }
    });
  });

  video.addEventListener('timeupdate', sync);
  video.addEventListener('seeked', sync);
  video.addEventListener('play', showState);
  video.addEventListener('pause', showState);
  setActive(0);
  showState();
})();
