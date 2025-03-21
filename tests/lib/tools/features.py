

# Will be removed
class AllFeature:
    name = "AllFeature"
    parent = "AllFeatures"
    
    @staticmethod
    def maybe_add_feature(feature_dict, json_entry: dict):
        feature_dict[AllFeature.parent].append(json_entry['MESSAGE'])
        return json_entry


# Will be removed
class NoneFeature:
    name = "NoneFeature"
    parent = "NoneFeatures"
    
    @staticmethod
    def maybe_add_feature(feature_dict, json_entry):
        pass
    

FEATURE_LIST = [AllFeature, NoneFeature]
