const logoAsset = {
  src: '/brand/doraigc-logo-mark-256.png',
  srcSet: '/brand/doraigc-logo-mark-256.png 1x, /brand/doraigc-logo-mark-512.png 2x'
};

export function BrandLogo({ compact = false }) {
  return (
    <div className={compact ? 'brand-logo brand-logo--compact' : 'brand-logo'}>
      <img
        src={logoAsset.src}
        srcSet={logoAsset.srcSet}
        alt="DORAIGC 标志"
      />
      {!compact && <span>DORAIGC</span>}
    </div>
  );
}
