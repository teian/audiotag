%{!?version:%global version 1.0.0}
Name: audiotag
Version: %{version}
Release: 1%{?dist}
Summary: Native lossless audio and audiobook metadata CLI
License: EUPL-1.2
Source0: %{name}-%{version}.tar.gz
AutoReqProv: no
%global debug_package %{nil}
%global __os_install_post %{nil}

%description
Standalone Go audio tag editor with audiobook workflows and shell completions.
No runtime dependencies.

%prep
%setup -q

%build
# Binary cross-compiled by scripts/package-rpm.sh.

%install
install -Dm755 audiotag %{buildroot}%{_bindir}/audiotag
install -Dm644 audiotag.bash %{buildroot}%{_datadir}/bash-completion/completions/audiotag
install -Dm644 audiotag.zsh %{buildroot}%{_datadir}/zsh/site-functions/_audiotag
install -Dm644 audiotag.fish %{buildroot}%{_datadir}/fish/vendor_completions.d/audiotag.fish

%files
%license LICENSE NOTICE Go-BSD-3-Clause.txt
%doc README.md TAGS.md BENCHMARKS.md CONTRIBUTING.md CLA.md examples source.tar.gz
%{_bindir}/audiotag
%{_datadir}/bash-completion/completions/audiotag
%{_datadir}/zsh/site-functions/_audiotag
%{_datadir}/fish/vendor_completions.d/audiotag.fish
