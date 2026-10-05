/* Progressive select presentation. Native controls own values/submission;
 * one bounded listbox is open at a time, without a framework or idle polling. */
let sequence = 0;
let active = null;
const states = new WeakMap();

function dismiss(focus = false) {
  if (!active) return;
  const {button, panel, wrapper, closeOverlays} = active;
  if (typeof panel.hidePopover === 'function' && panel.matches(':popover-open')) panel.hidePopover();
  panel.hidden = true; button.setAttribute('aria-expanded','false'); button.removeAttribute('aria-activedescendant');
  if (panel.parentElement!==wrapper) { if (wrapper.isConnected) wrapper.append(panel); else panel.remove(); }
  active = null;
  if (focus) closeOverlays();
  if (focus && button.isConnected) button.focus({preventScroll:true});
}

function bind(select, closeOverlays) {
  if (states.has(select) || select.multiple || !select.options.length || select.options.length > 100) return;
  const wrapper = document.createElement('span'); wrapper.className='select-control';
  const button = document.createElement('button'); button.type='button'; button.className='select select-trigger';
  const name = select.getAttribute('aria-label') || [...select.labels].map(label=>label.textContent.trim()).join(' ');
  button.setAttribute('role','combobox'); button.setAttribute('aria-label',name);
  button.setAttribute('aria-haspopup','listbox'); button.setAttribute('aria-expanded','false');
  const label=document.createElement('span'); label.className='select-value';
  const arrow=document.createElement('span'); arrow.className='select-chevron'; arrow.setAttribute('aria-hidden','true'); arrow.textContent='⌄';
  button.append(label,arrow);
  const panel=document.createElement('div'); panel.className='select-content'; panel.hidden=true;
  panel.id='select-list-'+(++sequence); panel.setAttribute('role','listbox'); panel.setAttribute('aria-label',name);
  button.setAttribute('aria-controls',panel.id);
  if (typeof panel.showPopover==='function') panel.setAttribute('popover','manual');
  const items=[...select.options].map((option,index)=>{
    const item=document.createElement('div'); item.className='select-item'; item.id=panel.id+'-'+index;
    item.setAttribute('role','option'); item.setAttribute('aria-selected','false');
    item.setAttribute('aria-disabled',String(option.disabled));
    const text=document.createElement('bdi'); text.textContent=option.textContent; text.dir=option.dir || 'auto';
    const check=document.createElement('span'); check.className='select-check'; check.setAttribute('aria-hidden','true'); check.textContent='✓';
    item.append(text,check);
    item.addEventListener('pointerdown',event=>event.preventDefault());
    item.addEventListener('pointermove',()=>{ if (!option.disabled && active===state) point(index); });
    item.addEventListener('click',()=>choose(index)); panel.append(item); return item;
  });
  let current=select.selectedIndex, typed='', typedAt=0;
  const state={select,button,panel,wrapper,closeOverlays}; states.set(select,state);
  const sync=()=>{
    label.textContent=select.selectedOptions[0]?.textContent || '';
    label.dir=select.selectedOptions[0]?.dir || 'auto';
    button.disabled=select.disabled;
    button.setAttribute('aria-invalid',select.getAttribute('aria-invalid') || 'false');
    const description=select.getAttribute('aria-describedby');
    if (description) button.setAttribute('aria-describedby',description); else button.removeAttribute('aria-describedby');
    items.forEach((item,index)=>item.setAttribute('aria-selected',String(index===select.selectedIndex)));
    if (button.disabled && active===state) dismiss();
  };
  const point=index=>{
    current=Math.max(0,Math.min(items.length-1,index));
    items.forEach((item,i)=>item.toggleAttribute('data-highlighted',i===current));
    button.setAttribute('aria-activedescendant',items[current].id);
    const item=items[current].getBoundingClientRect(), box=panel.getBoundingClientRect();
    if (item.top<box.top+4) panel.scrollTop-=box.top+4-item.top;
    else if (item.bottom>box.bottom-4) panel.scrollTop+=item.bottom-box.bottom+4;
  };
  const position=()=>{
    const rect=button.getBoundingClientRect();
    if (rect.bottom<0 || rect.top>innerHeight) { dismiss(); return; }
    panel.style.inlineSize=Math.min(Math.max(rect.width,180),innerWidth-16)+'px';
    const below=innerHeight-rect.bottom-14, above=rect.top-14;
    panel.style.maxBlockSize=Math.min(320,innerHeight-16,Math.max(88,below,above))+'px';
    const bounds=panel.getBoundingClientRect();
    panel.style.left=Math.max(8,Math.min(innerWidth-bounds.width-8,getComputedStyle(select).direction==='rtl'?rect.right-bounds.width:rect.left))+'px';
    panel.style.top=Math.max(8,Math.min(innerHeight-bounds.height-8,below>=bounds.height?rect.bottom+6:rect.top-bounds.height-6))+'px';
  };
  state.position=position;
  const open=()=>{
    if (button.disabled) return;
    dismiss(); closeOverlays(); sync(); active=state; panel.hidden=false; button.setAttribute('aria-expanded','true');
    if (typeof panel.showPopover==='function') panel.showPopover();
    // Native dialogs must contain their fallback popup; card blur/overflow must not clip it.
    else (select.closest('dialog') || document.body).append(panel);
    position();
    if (active!==state) return;
    point(Math.max(0,select.selectedIndex));
  };
  const choose=index=>{
    if (select.disabled || !select.options[index] || select.options[index].disabled) return;
    select.selectedIndex=index;
    select.dispatchEvent(new Event('input',{bubbles:true})); select.dispatchEvent(new Event('change',{bubbles:true}));
    sync(); dismiss(true);
  };
  const move=step=>{
    let next=current;
    for (let n=0;n<items.length;n++) { next=(next+step+items.length)%items.length; if (!select.options[next].disabled) break; }
    point(next);
  };
  button.addEventListener('click',()=>active===state?dismiss():open());
  button.addEventListener('keydown',event=>{
    if (event.key==='Tab') { dismiss(); return; }
    if (['ArrowDown','ArrowUp','Home','End'].includes(event.key)) {
      event.preventDefault(); if (active!==state) open();
      if (event.key==='Home'||event.key==='End') {
        const enabled=[...select.options].map((option,index)=>option.disabled?-1:index).filter(index=>index>=0);
        if (enabled.length) point(event.key==='Home'?enabled[0]:enabled.at(-1));
      } else move(event.key==='ArrowDown'?1:-1);
    } else if (event.key==='Enter'||event.key===' ') {
      event.preventDefault(); if (active===state) choose(current); else open();
    } else if (event.key.length===1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
      const now=Date.now(); typed=now-typedAt<700?typed+event.key:event.key; typedAt=now;
      if (active!==state) open();
      const index=[...select.options].findIndex(option=>!option.disabled&&option.textContent.trim().toLocaleLowerCase().startsWith(typed.toLocaleLowerCase()));
      if (index>=0) point(index);
    }
  });
  button.addEventListener('blur',()=>{ if (active===state) dismiss(); });
  select.addEventListener('change',()=>queueMicrotask(sync));
  select.addEventListener('ui:select-sync',()=>queueMicrotask(sync));
  select.form?.addEventListener('input',()=>queueMicrotask(sync));
  select.form?.addEventListener('change',()=>queueMicrotask(sync));
  select.form?.addEventListener('reset',()=>queueMicrotask(sync));
  select.closest('dialog')?.addEventListener('close',()=>{ if (active===state) dismiss(); });
  select.before(wrapper); wrapper.append(select,button,panel);
  select.hidden=true; sync();
  new MutationObserver(sync).observe(select,{attributes:true,attributeFilter:['disabled','aria-invalid','aria-describedby']});
}

export function enhanceSelects(root=document, closeOverlays=()=>{}) { root.querySelectorAll('select[data-fill-preset]').forEach(select=>bind(select,closeOverlays)); }
document.addEventListener('pointerdown',event=>{ if (active&&!active.wrapper.contains(event.target)&&!active.panel.contains(event.target)) dismiss(); },true);
// An open Select owns Escape before help or the enclosing dialog handles it.
window.addEventListener('keydown',event=>{ if (event.key==='Escape'&&active) { event.preventDefault(); event.stopPropagation(); dismiss(true); } },true);
window.addEventListener('resize',()=>dismiss());
document.addEventListener('scroll',event=>{ if (active&&!active.panel.contains(event.target)) active.position(); },true);
document.body.addEventListener('htmx:beforeSwap',()=>dismiss());
