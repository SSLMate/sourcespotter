(function() {
	let currentController = null;
	const intro = document.getElementById('deps-intro');
	const form = document.getElementById('deps-form');
	const errors = document.getElementById('deps-errors');
	const results = document.getElementById('deps-results');
	const badgeinfo = document.getElementById('deps-badgeinfo');
	const progress = document.getElementById('deps-progress');
	const configDetails = document.getElementById('deps-config');

	function getFormData() {
		let formData = new URLSearchParams(new FormData(form));
		if (formData.get('target') === 'linux/amd64') { formData.delete('target'); }
		if (formData.get('tags') === '') { formData.delete('tags'); }
		if (formData.get('firstparty') === '') { formData.delete('firstparty'); }
		return formData;
	}
	function setFormData(formData) {
		form['package'].value = formData.get('package') ?? '';
		form['target'].value = formData.get('target') ?? 'linux/amd64';
		form['tags'].value = formData.get('tags') ?? '';
		form['firstparty'].value = formData.get('firstparty') ?? '';
		form['test'].checked = formData.get('test') === '1';

		configDetails.open =
			form['target'].value !== 'linux/amd64' ||
			form['tags'].value !== '' ||
			form['test'].checked;
	}
	function loadState() {
		const formData = new URL(location).searchParams;
		setFormData(formData);
		loadResults(formData);
	}
	form.onsubmit = () => {
		const formData = getFormData();
		const url = new URL(location);
		url.search = '?' + formData.toString();
		history.pushState({}, "", url);
		loadResults(formData);
		return false;
	};
	window.addEventListener('popstate', loadState);
	loadState();

	async function loadResults(formData) {
		if (currentController) {
			currentController.abort();
			currentController = null;
		}

		let firstPartyWithSlash = formData.get('firstparty');
		if (firstPartyWithSlash && !firstPartyWithSlash.endsWith('/')) {
			firstPartyWithSlash += '/';
		}

		const pkg = formData.get('package') ?? '';
		if (pkg === '') {
			intro.hidden = false;
			errors.hidden = true;
			results.hidden = true;
			badgeinfo.hidden = true;
			progress.hidden = true;
			return;
		}

		intro.hidden = true;
		errors.hidden = true;
		results.hidden = true;
		badgeinfo.hidden = true;
		progress.hidden = false;
		const url = form.action + '?' + formData.toString();
		try {
			currentController = new AbortController();
			const resp = await fetch(url, {signal: currentController.signal});
			if (!resp.ok) {
				const body = await resp.text().catch(() => '');
				const msg = body || `Request to ${url} failed with status ${resp.status} ${resp.statusText}`;
				errors.textContent = msg;
				errors.hidden = false;
				results.hidden = true;
				badgeinfo.hidden = true;
				intro.hidden = true;
				progress.hidden = true;
				return;
			}

			const text = await resp.text();

			// Parse lines: MODULE PACKAGE
			const moduleMap = new Map();
			let mainModule = '';
			const lines = text.split(/\r?\n/);

			for (const line of lines) {
				const trimmed = line.trim();
				if (!trimmed) continue;

				const [depOnly, module, pkg] = trimmed.split(/\s+/, 3);
				if (!module || !pkg) continue;

				if (!moduleMap.has(module)) {
					moduleMap.set(module, []);
				}
				moduleMap.get(module).push(pkg);

				if (depOnly === 'false') {
					mainModule = module;
				}
			}
			moduleMap.delete(mainModule);

			// Build DOM for results
			results.innerHTML = '';
			const h2 = document.createElement('h2');
			const mainLink = document.createElement('a');
			mainLink.href = 'https://go-mod-viewer.appspot.com/' + mainModule;
			mainLink.textContent = mainModule;
			h2.appendChild(mainLink);
			if (moduleMap.size===0) {
				h2.appendChild(document.createTextNode(` has no dependencies`));
			} else {
				h2.appendChild(document.createTextNode(` depends on ${moduleMap.size} module${moduleMap.size===1?'':'s'}:`));
			}
			results.appendChild(h2);

			if (moduleMap.size > 0) {
				const resultsUL = document.createElement('ul');
				results.appendChild(resultsUL);

				for (const module of [...moduleMap.keys()].sort()) {
					const packages = moduleMap.get(module);
					const li = document.createElement('li');

					const details = document.createElement('details');
					const summary = document.createElement('summary');

					const aModule = document.createElement('a');
					aModule.href = 'https://go-mod-viewer.appspot.com/' + module;
					aModule.addEventListener('click', function(e) {
						e.stopPropagation();
					});
					aModule.textContent = module;
					const codeModule = document.createElement('code');
					codeModule.appendChild(aModule);

					const textNode = document.createTextNode(
						` (${packages.length} package${packages.length === 1 ? '' : 's'}) `
					);

					summary.appendChild(codeModule);
					summary.appendChild(textNode);
					if (module.startsWith("golang.org/")) {
						const badge = document.createElement('span');
						badge.className = 'deps-badge deps-badge-go';
						badge.textContent = 'Go';
						summary.appendChild(badge);
					} else if (firstPartyWithSlash && module.startsWith(firstPartyWithSlash)) {
						const badge = document.createElement('span');
						badge.className = 'deps-badge deps-badge-firstparty';
						badge.textContent = 'First-Party';
						summary.appendChild(badge);
					}
					details.appendChild(summary);

					const ul = document.createElement('ul');
					for (const p of packages.sort()) {
						const pkgLi = document.createElement('li');
						const codePkg = document.createElement('code');
						codePkg.textContent = p;
						pkgLi.appendChild(codePkg);
						ul.appendChild(pkgLi);
					}

					details.appendChild(ul);
					li.appendChild(details);
					resultsUL.appendChild(li);
				}
			}

			// Build DOM for badgeinfo
			badgeinfo.innerHTML = '';
			const badgeURL = `https://badges.api.${document.body.dataset.domain}/deps?${formData.toString()}`;
			let p = document.createElement('p');
			p.appendChild(document.createTextNode('Add this badge to your project website or README to help users understand your dependencies: '));
			let badgeLink = document.createElement('a');
			badgeLink.href = location.href;
			let badgeImg = document.createElement('img');
			badgeImg.src = badgeURL;
			badgeImg.alt = 'Dependencies';
			badgeLink.appendChild(badgeImg);
			p.appendChild(badgeLink);
			badgeinfo.appendChild(p);

			p = document.createElement('p');
			p.appendChild(document.createTextNode('HTML: '));
			let code = document.createElement('code');
			code.textContent = badgeLink.outerHTML;
			p.appendChild(code);
			badgeinfo.appendChild(p);

			p = document.createElement('p');
			p.appendChild(document.createTextNode('Markdown: '));
			code = document.createElement('code');
			code.textContent = `[![Dependencies](${badgeURL})](${location.href})`;
			p.appendChild(code);
			badgeinfo.appendChild(p);

			errors.hidden = true;
			results.hidden = false;
			badgeinfo.hidden = false;
			intro.hidden = true;
			progress.hidden = true;
		} catch (err) {
			if (!(err instanceof DOMException && err.name === "AbortError")) {
				errors.textContent = url + ": " + String(err);
				errors.hidden = false;
				results.hidden = true;
				badgeinfo.hidden = true;
				intro.hidden = true;
				progress.hidden = true;
			}
		}
	}
})();
